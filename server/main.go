// Command mk8 runs the Mario Kart 8 Deluxe online servers (auth + secure) on the
// Nextendo NEX stack — a from-scratch NEX implementation with no third-party
// dependencies.
//
// Two NEX servers run in one process:
//   - auth   (:443)   TicketGranting — LoginEx issues the Kerberos ticket.
//   - secure (:60003) SecureConnection + matchmaking + NAT-traversal + ranking + utility.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const (
	accessKey     = "09c1c475"
	nexVersion    = 40000
	securePID     = 2
	sessionKeyLen = 32
)

// securePassword : mot de passe Kerberos auth<->secure. Override par NEXTENDO_SECURE_PASSWORD
// en prod ; la valeur par defaut n est qu un placeholder de dev.
var securePassword = envOr("NEXTENDO_SECURE_PASSWORD", "securepasswordplz1")

var (
	nextendoHost = envOr("NEXTENDO_HOST", "127.0.0.1")
	authPort     = envOrInt("AUTH_PORT", 443)
	securePort   = envOrInt("SECURE_PORT", 60003)
	certFile     = envOr("CERT_FILE", "cert.pem")
	keyFile      = envOr("KEY_FILE", "key.pem")

	// nextendoSecret signs "nx2." NEX login tokens issued by the account service. It
	// MUST be byte-identical to nextendo-account's secret or token validation fails.
	// Match its loadSecret exactly: env NEXTENDO_SECRET as raw bytes, else hex-decode
	// the shared key file (the account has no env → it hex-decodes nextendo_secret.key).
	nextendoSecret = loadNextendoSecret()
	// requireAccount, when "1", rejects any login without a valid Nextendo token,
	// restricting the server to account holders.
	requireAccount = os.Getenv("NEXTENDO_REQUIRE_ACCOUNT") == "1"
)

func main() {
	if err := ensureLocalCertificate(certFile, keyFile); err != nil {
		fmt.Printf("[MK8] TLS setup failed: %v\n", err)
		return
	}
	nncsCfg := nncsConfigFromEnv()
	nncs, err := startNNCSResponders(nncsCfg)
	if err != nil {
		fmt.Printf("[MK8] NNCS setup failed: %v\n", err)
		return
	}
	defer nncs.Close()
	if secondaryIP := parseIPv4Env("NNCS_SECONDARY_IP", nil); secondaryIP != nil {
		secondaryCfg := nncsCfg
		secondaryCfg.ListenIP = secondaryIP
		secondaryCfg.PublicIP = secondaryIP
		secondaryCfg.SilentPort = 0
		secondaryCfg.FilterProbePort = 0

		secondary, err := startNNCSResponders(secondaryCfg)
		if err != nil {
			fmt.Printf("[MK8] secondary NNCS setup failed: %v\n", err)
			return
		}
		defer secondary.Close()
	}

	settings := nex.NewSwitchSettings(accessKey, nexVersion)

	// --- Auth server (insecure, :443) ---
	secureURL := nex.NewStationURL("prudp")
	secureURL.Set("address", nextendoHost)
	secureURL.SetInt("port", securePort)
	secureURL.SetInt("CID", 1)
	secureURL.SetInt("PID", securePID)
	secureURL.SetInt("sid", 1)
	secureURL.SetInt("stream", 10)
	secureURL.SetInt("type", 2) // public

	authEndpoint := nex.NewEndpoint(settings)
	authCfg := &nex.AuthConfig{
		Settings:         settings,
		SecurePID:        securePID,
		SecurePassword:   securePassword,
		SecureStationURL: secureURL,
		ServerName:       "Nextendo",
		SessionKeyLength: sessionKeyLen,
		ResolveUser:      resolveUser,
	}
	requestLimits := newRequestLimiter()
	rmcDedup := newRMCDeduplicator(time.Duration(envOrInt("MATCHMAKING_DEDUP_SECONDS", 20)) * time.Second)
	authEndpoint.Register(nex.ProtocolTicketGranting, wrapAuthRateLimit(authCfg.Handler(), requestLimits))
	authEndpoint.OnRMC = logRMC("Auth")
	authServer := nex.NewServer(authEndpoint)

	// --- Secure server (:60003) ---
	secureEndpoint := nex.NewEndpoint(settings)
	secureEndpoint.SetSecureAccount(securePassword, securePID)

	quality := nex.NewConnectionQualityRegistry()
	mm := nex.NewMatchmakingWithOptions(nex.MatchmakingOptions{
		CompatibilityAttributes: envOrInt("MATCHMAKING_COMPAT_ATTRIBUTES", 0),
		ReservationTTL:          time.Duration(envOrInt("MATCHMAKING_RESERVATION_SECONDS", 8)) * time.Second,
		DisconnectGrace:         time.Duration(envOrInt("MATCHMAKING_RECONNECT_GRACE_SECONDS", 20)) * time.Second,
		AdaptiveRelaxAfter:      time.Duration(envOrInt("MATCHMAKING_RELAX_AFTER_SECONDS", 8)) * time.Second,
		SearchIdleTTL:           time.Duration(envOrInt("MATCHMAKING_SEARCH_IDLE_SECONDS", 120)) * time.Second,
		SessionIdleTTL:          time.Duration(envOrInt("MATCHMAKING_SESSION_IDLE_SECONDS", 1200)) * time.Second,
		IntermissionGrace:       time.Duration(envOrInt("MATCHMAKING_INTERMISSION_GRACE_SECONDS", 20)) * time.Second,
		PairQualityScore:        quality.PairScore,
		HostScore:               quality.HostScore,
		QualityWeight:           int64(envOrInt("MATCHMAKING_QUALITY_WEIGHT", 3)),
		// Keep disabled until MK8D's client-side host-migration notification is
		// fully classified. Host scoring is active for safe post-disconnect migration.
		EnablePreRaceHostSelection: envBool("MATCHMAKING_PRE_RACE_HOST_SELECTION"),
		ConnectionIDForPID: func(pid uint64) uint32 {
			if c := secureEndpoint.FindConnectionByPID(pid); c != nil {
				return c.ID
			}
			return 0
		},
	})
	secureCfg := nex.SwitchPia519Config()
	secureCfg.OnStationUpdate = quality.Observe
	secureEndpoint.Register(nex.ProtocolSecureConnection, nex.SecureConnectionHandlerWithConfig(secureCfg))
	secureEndpoint.Register(nex.ProtocolMatchmakeExtension, rmcDedup.wrap(wrapMatchmakingRateLimit(mm.ExtensionHandler(), requestLimits)))
	secureEndpoint.Register(nex.ProtocolMatchMaking, rmcDedup.wrap(wrapMatchmakingRateLimit(mm.MatchMakingHandler(), requestLimits)))
	secureEndpoint.Register(nex.ProtocolMatchMakingExt, rmcDedup.wrap(wrapMatchmakingRateLimit(mm.MatchMakingExtHandler(), requestLimits)))
	secureEndpoint.Register(nex.ProtocolNATTraversal, nex.NATTraversalHandler())
	dataDir := envOr("NEXTENDO_DATA_DIR", "data")
	rankingStore, rankingStoreErr := nex.NewJSONLCommonDataStore(filepath.Join(dataDir, "ranking-common-data.jsonl"))
	if rankingStoreErr != nil {
		fmt.Printf("[Ranking] local persistence disabled: %v\n", rankingStoreErr)
	}
	secureEndpoint.Register(nex.ProtocolRanking, nex.RankingHandlerWithStore(rankingStore))
	secureEndpoint.Register(nex.ProtocolUtility, nex.UtilityHandler())
	redisState, redisErr := newRedisStatePublisher(secureEndpoint, mm)
	if redisErr != nil {
		fmt.Printf("[Redis] setup failed: %v\n", redisErr)
		return
	}
	if redisState != nil {
		redisState.Start()
		defer redisState.Close()
	}
	logSecure := logRMC("Secure")
	secureEndpoint.OnRMC = func(c *nex.Connection, req *nex.RMCMessage) {
		logSecure(c, req)
		noteRMC(c, req) // feed the monitoring dashboard
	}
	secureEndpoint.OnConnect = func(c *nex.Connection) {
		fmt.Printf("[MK8 Secure] connected pid=%d id=%d addr=%s\n", c.PID, c.ID, c.RemoteAddr)
		redisState.ConnectionChanged("connected", c)
	}
	// Drop the player's lobbies when the connection dies. A gathering is otherwise only
	// removed when the client politely calls UnregisterGathering / EndParticipation, so a
	// client that crashes or errors out leaks its lobby forever: the monitoring fills with
	// phantom lobbies "searching" for a player who is long gone, and matchmaking can hand
	// those dead sessions to real players.
	secureEndpoint.OnDisconnect = func(c *nex.Connection) {
		mm.MarkPlayerDisconnected(c.PID)
		redisState.ConnectionChanged("disconnected", c)
	}
	secureServer := nex.NewServer(secureEndpoint)

	// Monitoring: per-game /api/stats for the unified Nextendo dashboard.
	// Éviction automatique des connexions mortes. Sans elle, une session perdue (crash,
	// coupure, émulateur fermé) restait enregistrée indéfiniment : le joueur se voyait
	// refuser l'accès (« ce compte joue déjà ailleurs ») et les compteurs étaient faux.
	secureEndpoint.StartReaper()
	go startMatchmakingJanitor(mm)
	go startDashboard(secureEndpoint, mm, redisState)

	// When the auth is fronted by a TLS-passthrough proxy (Traefik on the shared :443),
	// enable PROXY protocol so the auth sees the console's REAL IP. Without it the login PID is
	// remembered under Traefik's internal IP (10.0.1.x), and MK8's TICKETLESS secure CONNECT —
	// which arrives on the host-published :60003 with the real client IP — can't RecallAuthPID it,
	// falling back to an incrementing placeholder PID (1800000001, 1800000002, ...) the console
	// doesn't recognise as itself -> Pia self-recognition fails -> SessionKeepFailed / comm error.
	proxyProto := os.Getenv("NEXTENDO_PROXY_PROTOCOL") == "1"
	go func() {
		fmt.Printf("[MK8 Auth] listening WSS :%d (proxyProto=%v, secure URL -> %s)\n", authPort, proxyProto, secureURL.String())
		var err error
		if proxyProto {
			err = authServer.ListenSecureProxy(authPort, certFile, keyFile)
		} else {
			err = authServer.ListenSecure(authPort, certFile, keyFile)
		}
		if err != nil {
			fmt.Printf("[MK8 Auth] stopped: %v\n", err)
		}
	}()

	fmt.Printf("[MK8 Secure] listening WSS :%d\n", securePort)
	if err := secureServer.ListenSecure(securePort, certFile, keyFile); err != nil {
		fmt.Printf("[MK8 Secure] stopped: %v\n", err)
	}
}

func startMatchmakingJanitor(mm *nex.Matchmaking) {
	if mm == nil {
		return
	}
	interval := time.Duration(envOrInt("MATCHMAKING_JANITOR_SECONDS", 15)) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if removed := mm.PruneExpiredSessions(); removed != 0 {
			fmt.Printf("[MM] janitor removed %d stale gathering(s)\n", removed)
		}
	}
}

// resolveUser maps a LoginEx username to an account. A valid "nx2." Nextendo
// token resolves to its persistent PID; anything else gets a stable anonymous
// PID derived from the username (so the same console keeps the same identity).
func resolveUser(username string, extraData []byte) (uint64, []byte, bool) {
	// The source key encrypts the client ticket and is handed back as pSourceKey,
	// so the console decrypts it. It MUST be 32 bytes (the Switch kerberos key
	// size) — a 16-byte key makes the console reject the ticket. Derive it
	// deterministically per user.
	sk := sha256.Sum256([]byte("nextendo-src:" + username))
	sourceKey := sk[:]

	// Current Ryujinx builds place the account service's signed nx2 token in the
	// custom "nnex" claim of the BAAS JWT that MK8D forwards in LoginEx extraData.
	// Bind that proof to the numeric username before accepting the account PID.
	// The outer JWT signature is not trusted here; nx2 has its own HMAC and expiry.
	if pid, present, valid := signedPIDForUsername(username, extraData); present {
		if !valid {
			fmt.Printf("[Auth] username=%q REFUSED: invalid signed proof\n", username)
			return 0, nil, false
		}
		if allow, reason := nextendoOnlineCheck(pid, "ryujinx"); !allow {
			fmt.Printf("[Auth] pid=%d online REFUSED (%s)\n", pid, reason)
			return 0, nil, false
		}
		fmt.Printf("[Auth] pid=%d verified by signed LoginEx proof\n", pid)
		return pid, sourceKey, true
	}

	// 1. Signed nx2 token → the account's PERSISTENT PID (+ online gates).
	if pid, ok := nextendoPIDFromToken(username); ok {
		if allow, reason := nextendoOnlineCheck(pid, "ryujinx"); !allow {
			fmt.Printf("[Auth] pid=%d online REFUSÉ (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	// 2. Numeric username. The emulator's "Connexion Nextendo" button sends the
	// account's OWN PID (a bare number in the Nextendo range) as the username; a REAL
	// CFW Switch sends its console baasUserID (a large NSA id) instead, which we must
	// resolve to the account PID. Using the account PID verbatim keeps the NEX identity
	// = the account the game knows itself by (hashing it breaks Pia's self-recognition
	// → 2618-562 SessionKeepFailed).
	if n, err := strconv.ParseUint(username, 10, 64); err == nil && n >= 1800000000 {
		// FAILLE D AUTHENTIFICATION CONNUE. Ce chemin accepte un PID NU comme identite :
		// aucun jeton, aucune signature. Les PID etant sequentiels depuis 1800000001, il
		// suffit d envoyer le numero d un autre membre pour jouer sous son identite — et,
		// via la garde « un seul endroit », l empecher lui-meme de jouer.
		// On ne peut pas l interdire sechement : l emulateur distribue envoie precisement
		// ce PID nu. Le refus est donc derriere un interrupteur, a activer quand une build
		// envoyant le jeton nx2 signe sera deployee. En attendant on journalise chaque usage.
		if requireSignedToken() {
			fmt.Printf("[Auth] pid=%d REFUSE : identite par PID nu desactivee (jeton nx2 signe requis)\n", n)
			return 0, nil, false
		}
		fmt.Printf("[Auth] pid=%d identite par PID NU (non authentifiee — cf. NEXTENDO_REQUIRE_SIGNED_TOKEN)\n", n)
		pid, kind := n, "ryujinx"
		if n >= 1810000000 { // vraie Switch : NSA id -> PID de compte (online = comptes Nextendo UNIQUEMENT)
			kind = "switch"
			rp, st := resolveNSAtoPID(n)
			switch st {
			case nsaOK:
				pid = rp
				fmt.Printf("[Auth] NSA %d -> account pid=%d\n", n, pid)
			case nsaUnknown:
				fmt.Printf("[Auth] NSA %d REFUSÉ (aucun compte Nextendo)\n", n)
				return 0, nil, false
			case nsaUnreachable:
				fmt.Printf("[Auth] NSA %d REFUSÉ (serveur compte injoignable)\n", n)
				return 0, nil, false
			}
		}
		// GATES online : #6 e-mail vérifié + #5 un seul endroit + compte inconnu/désactivé.
		if allow, reason := nextendoOnlineCheck(pid, kind); !allow {
			fmt.Printf("[Auth] pid=%d online REFUSÉ (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	// 3. Anonymous / no Nextendo identity. When requireAccount is on, online REQUIRES
	// a Nextendo account → reject (the game can't enter online mode).
	if requireAccount {
		fmt.Printf("[Auth] login anonyme REFUSÉ (compte Nextendo requis): %q\n", username)
		return 0, nil, false
	}
	return anonymousPID(username), sourceKey, true
}

func signedPIDForUsername(username string, extraData []byte) (pid uint64, present, valid bool) {
	proof, present := nextendoProofFromExtraData(extraData)
	if !present {
		return 0, false, false
	}
	pid, valid = nextendoPIDFromToken(proof)
	if !valid {
		return 0, true, false
	}
	claimed, err := strconv.ParseUint(username, 10, 64)
	return pid, true, err == nil && claimed == pid
}

// nextendoProofFromExtraData locates a BAAS JWT in opaque LoginEx extraData and
// returns its nnex claim. We only decode the JWT payload: authenticity comes
// from validating the embedded nx2 HMAC, never from trusting this outer token.
func nextendoProofFromExtraData(extraData []byte) (string, bool) {
	const maxExtraData = 64 * 1024
	if len(extraData) > maxExtraData {
		return "", false
	}
	for start := 0; start < len(extraData); start++ {
		if start+3 > len(extraData) || string(extraData[start:start+3]) != "eyJ" {
			continue
		}
		end := start
		for end < len(extraData) && jwtByte(extraData[end]) {
			end++
		}
		candidate := string(extraData[start:end])
		parts := strings.Split(candidate, ".")
		if len(parts) != 3 {
			continue
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			continue
		}
		var claims struct {
			Proof string `json:"nnex"`
		}
		if json.Unmarshal(payload, &claims) == nil && claims.Proof != "" {
			return claims.Proof, true
		}
	}
	return "", false
}

func jwtByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.'
}

// nextendoPIDFromToken validates a "nx2.<b64(pid.username.expiry)>.<b64(hmac)>"
// token signed by the account service (HMAC-SHA256, "nex:" prefix).
func nextendoPIDFromToken(s string) (uint64, bool) {
	if len(nextendoSecret) == 0 || !strings.HasPrefix(s, "nx2.") {
		return 0, false
	}
	parts := strings.Split(s[len("nx2."):], ".")
	if len(parts) != 2 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, nextendoSecret)
	mac.Write([]byte("nex:" + string(raw)))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return 0, false
	}
	f := strings.SplitN(string(raw), ".", 3) // pid.username.expiry
	if len(f) != 3 {
		return 0, false
	}
	pid, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0, false
	}
	if exp, err := strconv.ParseInt(f[2], 10, 64); err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	return pid, true
}

// loadNextendoSecret loads the shared NEX-token signing secret the SAME way
// nextendo-account does (its loadSecret): env NEXTENDO_SECRET as raw bytes if set,
// otherwise hex-decode the shared key file (NEXTENDO_SECRET_FILE, default
// nextendo_secret.key). The deployed account has no env → it hex-decodes the file,
// so the game server must too or the HMAC won't match and every nx2 token is rejected.
func loadNextendoSecret() []byte {
	if v := os.Getenv("NEXTENDO_SECRET"); v != "" {
		return []byte(v)
	}
	path := envOr("NEXTENDO_SECRET_FILE", "nextendo_secret.key")
	if b, err := os.ReadFile(path); err == nil {
		if dec, derr := hex.DecodeString(strings.TrimSpace(string(b))); derr == nil && len(dec) >= 16 {
			return dec
		}
	}
	return nil
}

// anonymousPID derives a stable PID in the NEX user range from a username.
func anonymousPID(username string) uint64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(username))
	return 1800000000 + uint64(h.Sum32()%100000000)
}

func logRMC(tag string) func(*nex.Connection, *nex.RMCMessage) {
	return func(c *nex.Connection, req *nex.RMCMessage) {
		fmt.Printf("[MK8 %s] pid=%d proto=%#x method=%d call=%d\n", tag, c.PID, req.Protocol, req.Method, req.CallID)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// requireSignedToken : quand true, seule une identite prouvee par un jeton nx2 SIGNE est
// acceptee au LoginEx ; un PID nu est refuse. Desactive par defaut car l emulateur
// actuellement distribue envoie encore le PID nu — a activer apres la prochaine release.
func requireSignedToken() bool {
	v := os.Getenv("NEXTENDO_REQUIRE_SIGNED_TOKEN")
	return v == "1" || v == "true"
}
