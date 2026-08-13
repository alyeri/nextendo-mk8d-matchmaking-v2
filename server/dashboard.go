package main

// dashboard.go exposes a per-game /api/stats JSON for the unified Nextendo
// monitoring site (nextendo-dashboard, :8085). The aggregator polls this endpoint,
// tags it "mk8" and rolls it into the global view. the game server keeps all state in memory
// (no Postgres), so we read the live gatherings + connections through the nex
// package's read-only Snapshot accessors, plus lightweight RMC counters fed from
// the endpoint's OnRMC hook. JSON keys mirror the proven mk8 dashboard so the
// existing aggregator UI renders it unchanged.

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

var (
	dashStart = time.Now()

	metaMu        sync.Mutex
	playerMeta    = map[uint64]*playerInfo{}
	sessionsSeen  int64
	peakConnected int

	rmcTotal int64

	eventsMu    sync.Mutex
	events      []rmcEvent // ring buffer of recent RMC calls (newest last)
	methodCount = map[string]int64{}
)

type playerInfo struct {
	PID        uint64
	FirstSeen  time.Time
	LastSeen   time.Time
	IP         string
	Calls      int64
	LastProto  uint16
	LastMethod uint32
}

type rmcEvent struct {
	T      time.Time
	PID    uint64
	Proto  uint16
	Method uint32
}

// noteRMC records an RMC call (fed from endpoint.OnRMC): updates the player, the
// global counters, the per-method totals and the live event ring.
func noteRMC(c *nex.Connection, req *nex.RMCMessage) {
	pid := c.PID
	if pid == 0 {
		return
	}
	atomic.AddInt64(&rmcTotal, 1)

	metaMu.Lock()
	pi := playerMeta[pid]
	if pi == nil {
		pi = &playerInfo{PID: pid, FirstSeen: time.Now()}
		playerMeta[pid] = pi
		sessionsSeen++
	}
	pi.LastSeen = time.Now()
	pi.Calls++
	pi.LastProto = req.Protocol
	pi.LastMethod = req.Method
	if c.RemoteAddr != "" {
		pi.IP = c.RemoteAddr
	}
	metaMu.Unlock()

	eventsMu.Lock()
	events = append(events, rmcEvent{T: time.Now(), PID: pid, Proto: req.Protocol, Method: req.Method})
	if len(events) > 100 {
		events = events[len(events)-100:]
	}
	methodCount[rmcName(req.Protocol, req.Method)]++
	eventsMu.Unlock()
}

// gameModeName maps MK8 online MatchmakeSession game_mode -> a human label.
func gameModeName(mode uint32) string {
	switch mode {
	case 1:
		return "Course"
	case 2:
		return "Course (équipe)"
	case 3:
		return "Bataille"
	case 4:
		return "Bataille (équipe)"
	default:
		return fmt.Sprintf("Mode %d", mode)
	}
}

func rmcName(proto uint16, method uint32) string {
	pn := map[uint16]string{
		0x0A: "TicketGranting", 0x0B: "SecureConnection", 0x6E: "Utility",
		0x70: "Ranking", 0x6D: "MatchmakeExt", 0x15: "MatchMaking",
		0x32: "MatchMakingExt", 0x03: "NATTraversal", 0x0E: "Notifications",
	}[proto]
	if pn == "" {
		pn = fmt.Sprintf("Proto-0x%X", proto)
	}
	mn := ""
	switch proto {
	case 0x0A:
		mn = map[uint32]string{1: "Login", 2: "LoginEx", 3: "RequestTicket"}[method]
	case 0x0B:
		mn = map[uint32]string{1: "Register", 4: "RegisterEx", 7: "ReplaceURL"}[method]
	case 0x6E:
		mn = map[uint32]string{1: "AcquireNexUniqueID", 7: "GetIntegerSettings", 8: "GetStringSettings"}[method]
	case 0x70:
		mn = map[uint32]string{1: "UploadScore", 4: "UploadCommonData", 6: "GetCommonData"}[method]
	case 0x6D:
		mn = map[uint32]string{
			0x26: "CreateMatchmakeSessionWithParam", 0x27: "JoinMatchmakeSessionWithParam",
			0x28: "AutoMatchmakeWithParamPostpone", 0x22: "UpdateProgressScore",
			0x31: "FindMatchmakeSessionBySingleID", 0x3C: "GetSimplePlayingSession",
			0x44: "PrivateRoomCreate", 0x45: "ResolveCode",
		}[method]
	case 0x15:
		mn = map[uint32]string{0x02: "UnregisterGathering", 0x29: "GetSessionURLs", 0x15: "FindBySingleID"}[method]
	case 0x32:
		mn = map[uint32]string{0x01: "EndParticipation"}[method]
	case 0x03:
		mn = map[uint32]string{2: "InitiateProbe", 3: "RequestProbeInitiationExt", 5: "ReportNATProperties"}[method]
	}
	if mn == "" {
		mn = fmt.Sprintf("m%d", method)
	}
	return pn + "::" + mn
}

// ----- stats JSON (keys mirror the proven mk8 dashboard) --------------------

type apiPlayer struct {
	PID        uint64 `json:"pid"`
	Name       string `json:"name"`
	IP         string `json:"ip"`
	State      string `json:"state"`
	Gathering  uint32 `json:"gathering"`
	OnlineSecs int    `json:"onlineSeconds"`
	Calls      int64  `json:"calls"`
	LastAction string `json:"lastAction"`
	IdleSecs   int    `json:"idleSeconds"`
	VR         uint32 `json:"vr"`
	Mode       string `json:"mode"`
	IsHost     bool   `json:"isHost"`
}

type apiLobbyP struct {
	PID  uint64 `json:"pid"`
	Name string `json:"name"`
	VR   uint32 `json:"vr"`
	Host bool   `json:"host"`
}

type apiGathering struct {
	ID       uint32      `json:"id"`
	HostPID  uint64      `json:"hostPid"`
	HostName string      `json:"hostName"`
	Type     string      `json:"type"`
	Mode     uint32      `json:"mode"`
	VR       uint32      `json:"vr"`
	Players  []apiLobbyP `json:"players"`
	Count    int         `json:"count"`
	Max      uint16      `json:"max"`
	State    string      `json:"state"`
	Code     string      `json:"code,omitempty"`
}

type apiEvent struct {
	Ago    int    `json:"agoSeconds"`
	PID    uint64 `json:"pid"`
	Action string `json:"action"`
}

type apiMethod struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type apiServer struct {
	AccessKey  string `json:"accessKey"`
	NexVersion string `json:"nexVersion"`
	AuthPort   string `json:"authPort"`
	SecurePort int    `json:"securePort"`
	SNIHost    string `json:"sniHost"`
	SessionKey int    `json:"sessionKeyLen"`
	Stack      string `json:"stack"`
}

type apiStats struct {
	ServerTime     string         `json:"serverTime"`
	UptimeSeconds  int            `json:"uptimeSeconds"`
	Connected      int            `json:"connected"`
	InLobby        int            `json:"inLobby"`
	ActiveLobbies  int            `json:"activeLobbies"`
	TotalSessions  int64          `json:"totalSessions"`
	TotalRMC       int64          `json:"totalRmc"`
	GatheringsMade int64          `json:"gatheringsMade"`
	PeakConnected  int            `json:"peakConnected"`
	Server         apiServer      `json:"server"`
	Players        []apiPlayer    `json:"players"`
	Gatherings     []apiGathering `json:"gatherings"`
	Events         []apiEvent     `json:"events"`
	Methods        []apiMethod    `json:"methods"`
}

func dispName(pid uint64) string { return fmt.Sprintf("Joueur-%d", pid%100000) }

func buildStats(endpoint *nex.Endpoint, mm *nex.Matchmaking) apiStats {
	conns := endpoint.SnapshotConnections()
	gaths := mm.Snapshot()

	// Snapshot per-player metadata under the lock.
	type metaSnap struct {
		calls       int64
		first, last time.Time
		proto       uint16
		meth        uint32
		ip          string
	}
	metaMu.Lock()
	snap := make(map[uint64]metaSnap, len(playerMeta))
	for pid, pi := range playerMeta {
		snap[pid] = metaSnap{calls: pi.Calls, first: pi.FirstSeen, last: pi.LastSeen, proto: pi.LastProto, meth: pi.LastMethod, ip: pi.IP}
	}
	metaMu.Unlock()

	// Live lobbies -> map each participant to its gathering / mode / vr / host flag.
	pidGathering := map[uint64]uint32{}
	pidMode := map[uint64]uint32{}
	pidVR := map[uint64]uint32{}
	pidIsHost := map[uint64]bool{}
	gs := make([]apiGathering, 0, len(gaths))
	for _, g := range gaths {
		if len(g.Participants) == 0 {
			continue
		}
		max := g.MaxPart
		if max == 0 {
			max = 12
		}
		state := "en recherche"
		if len(g.Participants) >= 2 {
			state = "apparié"
		}
		lps := make([]apiLobbyP, 0, len(g.Participants))
		for _, p := range g.Participants {
			pidGathering[p] = g.ID
			pidMode[p] = g.GameMode
			pidVR[p] = g.VR
			host := p == g.HostPID
			if host {
				pidIsHost[p] = true
			}
			lps = append(lps, apiLobbyP{PID: p, Name: dispName(p), VR: g.VR, Host: host})
		}
		gs = append(gs, apiGathering{
			ID: g.ID, HostPID: g.HostPID, HostName: dispName(g.HostPID),
			Type: gameModeName(g.GameMode), Mode: g.GameMode, VR: g.VR,
			Players: lps, Count: len(g.Participants), Max: max, State: state, Code: g.Code,
		})
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].ID < gs[j].ID })

	// Players from live connections (deduped by PID).
	players := make([]apiPlayer, 0, len(conns))
	inLobby := 0
	seen := map[uint64]bool{}
	for _, c := range conns {
		pid := c.PID
		if pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		s := snap[pid]
		online, idle, last := 0, 0, ""
		if !s.first.IsZero() {
			online = int(time.Since(s.first).Seconds())
			idle = int(time.Since(s.last).Seconds())
			last = rmcName(s.proto, s.meth)
		}
		ip := c.Addr
		if ip == "" {
			ip = s.ip
		}
		gid := pidGathering[pid]
		state := "en ligne"
		if gid != 0 {
			inLobby++
			state = "dans un lobby"
		}
		modeLabel := ""
		if m := pidMode[pid]; m != 0 {
			modeLabel = gameModeName(m)
		}
		players = append(players, apiPlayer{
			PID: pid, Name: dispName(pid), IP: ip, State: state, Gathering: gid,
			OnlineSecs: online, Calls: s.calls, LastAction: last, IdleSecs: idle,
			VR: pidVR[pid], Mode: modeLabel, IsHost: pidIsHost[pid],
		})
	}
	sort.Slice(players, func(i, j int) bool { return players[i].PID < players[j].PID })

	if len(players) > peakConnected {
		peakConnected = len(players)
	}

	// Recent events (newest first) + method totals.
	eventsMu.Lock()
	ev := make([]apiEvent, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		ev = append(ev, apiEvent{Ago: int(time.Since(e.T).Seconds()), PID: e.PID, Action: rmcName(e.Proto, e.Method)})
	}
	ms := make([]apiMethod, 0, len(methodCount))
	for n, c := range methodCount {
		ms = append(ms, apiMethod{Name: n, Count: c})
	}
	eventsMu.Unlock()
	sort.Slice(ms, func(i, j int) bool { return ms[i].Count > ms[j].Count })

	return apiStats{
		ServerTime:     time.Now().Format("15:04:05"),
		UptimeSeconds:  int(time.Since(dashStart).Seconds()),
		Connected:      len(players),
		InLobby:        inLobby,
		ActiveLobbies:  len(gs),
		TotalSessions:  atomic.LoadInt64(&sessionsSeen),
		TotalRMC:       atomic.LoadInt64(&rmcTotal),
		GatheringsMade: int64(len(gs)),
		PeakConnected:  peakConnected,
		Server: apiServer{
			AccessKey: accessKey, NexVersion: "4.0.0", AuthPort: fmt.Sprintf("%d", authPort),
			SecurePort: securePort, SNIHost: envOr("NEXTENDO_SNI_HOST", ""), SessionKey: sessionKeyLen,
			Stack: "nextendo-nex",
		},
		Players:    players,
		Gatherings: gs,
		Events:     ev,
		Methods:    ms,
	}
}

func writePrometheusMetrics(w http.ResponseWriter, endpoint *nex.Endpoint, mm *nex.Matchmaking) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	stats := buildStats(endpoint, mm)
	matchmaking := mm.Metrics()
	nncs := snapshotNNCSMetrics()

	fmt.Fprintln(w, "# HELP nextendo_mk8d_uptime_seconds Process uptime in seconds.")
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_uptime_seconds gauge")
	fmt.Fprintf(w, "nextendo_mk8d_uptime_seconds %d\n", stats.UptimeSeconds)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_connected_players gauge")
	fmt.Fprintf(w, "nextendo_mk8d_connected_players %d\n", stats.Connected)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_active_lobbies gauge")
	fmt.Fprintf(w, "nextendo_mk8d_active_lobbies %d\n", stats.ActiveLobbies)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_players_in_lobbies gauge")
	fmt.Fprintf(w, "nextendo_mk8d_players_in_lobbies %d\n", stats.InLobby)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_rmc_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_rmc_total %d\n", stats.TotalRMC)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_sessions_seen_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_sessions_seen_total %d\n", stats.TotalSessions)

	fmt.Fprintln(w, "# TYPE nextendo_mk8d_matchmaking_gatherings_created_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_matchmaking_gatherings_created_total %d\n", matchmaking.GatheringsCreated)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_matchmaking_joins_committed_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_matchmaking_joins_committed_total %d\n", matchmaking.JoinsCommitted)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_matchmaking_reservations_rejected_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_matchmaking_reservations_rejected_total %d\n", matchmaking.ReservationsRejected)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_matchmaking_candidates_selected_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_matchmaking_candidates_selected_total %d\n", matchmaking.CandidatesSelected)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_reconnect_leases_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_reconnect_leases_total %d\n", matchmaking.ReconnectLeases)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_reconnect_recovered_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_reconnect_recovered_total %d\n", matchmaking.ReconnectRecovered)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_disconnect_evictions_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_disconnect_evictions_total %d\n", matchmaking.DisconnectEvictions)

	phaseCounts := map[nex.SessionPhase]int{}
	for _, session := range mm.SessionInfos() {
		phaseCounts[session.Phase]++
	}
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_lobbies_by_phase gauge")
	for phase := nex.SessionSearching; phase <= nex.SessionClosing; phase++ {
		fmt.Fprintf(w, "nextendo_mk8d_lobbies_by_phase{phase=%q} %d\n", phase.String(), phaseCounts[phase])
	}

	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_valid_requests_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_valid_requests_total %d\n", nncs.ValidRequests)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_invalid_requests_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_invalid_requests_total %d\n", nncs.InvalidRequests)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_replies_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_replies_total %d\n", nncs.RepliesSent)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_reply_errors_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_reply_errors_total %d\n", nncs.ReplyErrors)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_filter_probes_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_filter_probes_total %d\n", nncs.FilterProbesSent)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_observations_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_observations_total %d\n", nncs.ObservationsSaved)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_observation_errors_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_observation_errors_total %d\n", nncs.ObservationErrors)
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_nncs_silent_packets_total counter")
	fmt.Fprintf(w, "nextendo_mk8d_nncs_silent_packets_total %d\n", nncs.SilentPackets)

	eventsMu.Lock()
	methods := make(map[string]int64, len(methodCount))
	for name, count := range methodCount {
		methods[name] = count
	}
	eventsMu.Unlock()
	fmt.Fprintln(w, "# TYPE nextendo_mk8d_rmc_method_total counter")
	for name, count := range methods {
		fmt.Fprintf(w, "nextendo_mk8d_rmc_method_total{method=%q} %d\n", name, count)
	}
}

type apiRoomsResponse struct {
	SchemaVersion int               `json:"schemaVersion"`
	UpdatedAt     string            `json:"updatedAt"`
	Redis         map[string]any    `json:"redis"`
	Rooms         []redisRoomRecord `json:"rooms"`
}

func buildRoomsResponse(mm *nex.Matchmaking, redisState *redisStatePublisher) apiRoomsResponse {
	now := time.Now().UTC()
	enabled, healthy, instance := redisState.status()
	snapshot, _ := buildRedisRoomSnapshot(instance, now, mm.SessionInfos())
	return apiRoomsResponse{
		SchemaVersion: 1,
		UpdatedAt:     now.Format(time.RFC3339Nano),
		Redis:         map[string]any{"enabled": enabled, "healthy": healthy, "instance": instance},
		Rooms:         snapshot.Rooms,
	}
}

func dashboardMux(endpoint *nex.Endpoint, mm *nex.Matchmaking, redisState *redisStatePublisher, token string) *http.ServeMux {
	// SÉCURITÉ : sans jeton configuré on REFUSE, au lieu d'ouvrir l'API à tout le monde.
	// L'ancienne condition (token == "") laissait la liste des joueurs — pseudos, PID et
	// adresses IP — lisible sans authentification dès que la variable manquait.
	// Comparaison à temps constant : un test == fuit la longueur du préfixe correct.
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		const prefix = "Bearer "
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		provided := ""
		if strings.HasPrefix(header, prefix) {
			provided = strings.TrimSpace(strings.TrimPrefix(header, prefix))
		}
		if token != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1 {
			return true
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="nextendo-admin"`)
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(buildStats(endpoint, mm))
	})
	mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(buildRoomsResponse(mm, redisState))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		writePrometheusMetrics(w, endpoint, mm)
	})
	// /api/kick — libère un compte resté coincé derrière une connexion morte, sans
	// redémarrer le serveur (ce qui déconnecterait tous les joueurs en partie).
	//   ?pid=<PID>     déconnecte toutes les connexions de ce compte
	//   ?rvcid=<id>    déconnecte une connexion précise
	//   (sans param.)  évince immédiatement toutes les connexions mortes
	mux.HandleFunc("/api/kick", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		var body struct {
			PID   uint64 `json:"pid"`
			RVCID uint32 `json:"rvcid"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil && err != io.EOF {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if body.PID != 0 && body.RVCID != 0 {
			http.Error(w, "provide either pid or rvcid", http.StatusBadRequest)
			return
		}
		if body.PID != 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{"pid": body.PID, "kicked": endpoint.KickPID(body.PID)})
			return
		}
		if body.RVCID != 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{"rvcid": body.RVCID, "kicked": endpoint.KickConnection(body.RVCID)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"reaped": endpoint.ReapIdle(nex.ReapIdleTimeout())})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	return mux
}

// startDashboard binds to loopback by default. Public access should go through
// an explicitly authenticated reverse proxy or an SSH tunnel, never a cloud
// firewall rule exposing the administrative port directly.
func startDashboard(endpoint *nex.Endpoint, mm *nex.Matchmaking, redisState *redisStatePublisher) {
	port := envOr("DASH_PORT", "8082")
	bind := envOr("DASH_BIND", "127.0.0.1")
	token := envOr("DASH_TOKEN", "")
	mux := dashboardMux(endpoint, mm, redisState, token)
	address := net.JoinHostPort(bind, port)

	fmt.Printf("[MK8 Dashboard] stats API on %s (bearer=%v)\n", address, token != "")
	if err := http.ListenAndServe(address, mux); err != nil {
		fmt.Printf("[MK8 Dashboard] HTTP error: %v\n", err)
	}
}
