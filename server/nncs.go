package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	nncsDefaultProbePort  = 10025
	nncsDefaultAltPort    = 10125
	nncsDefaultSilentPort = 33334
	nncsDefaultFilterPort = 50920
	nncsPacketSize        = 16
	nncsMaxPacketSize     = 64
)

type nncsConfig struct {
	ListenIP        net.IP
	PublicIP        net.IP
	ProbePorts      []int
	SilentPort      int
	FilterProbePort int
	ObservationFile string
}

type nncsResponders struct {
	sockets []*net.UDPConn
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
	writer  *nncsObservationWriter
}

type nncsObservationWriter struct {
	path string
	mu   sync.Mutex
}

type nncsMetricCounters struct {
	validRequests     atomic.Uint64
	invalidRequests   atomic.Uint64
	repliesSent       atomic.Uint64
	replyErrors       atomic.Uint64
	filterProbesSent  atomic.Uint64
	observationsSaved atomic.Uint64
	observationErrors atomic.Uint64
	silentPackets     atomic.Uint64
}

type nncsMetrics struct {
	ValidRequests, InvalidRequests, RepliesSent, ReplyErrors uint64
	FilterProbesSent, ObservationsSaved, ObservationErrors   uint64
	SilentPackets                                            uint64
}

var nncsCounters nncsMetricCounters

func snapshotNNCSMetrics() nncsMetrics {
	return nncsMetrics{
		ValidRequests: nncsCounters.validRequests.Load(), InvalidRequests: nncsCounters.invalidRequests.Load(),
		RepliesSent: nncsCounters.repliesSent.Load(), ReplyErrors: nncsCounters.replyErrors.Load(),
		FilterProbesSent: nncsCounters.filterProbesSent.Load(), ObservationsSaved: nncsCounters.observationsSaved.Load(),
		ObservationErrors: nncsCounters.observationErrors.Load(), SilentPackets: nncsCounters.silentPackets.Load(),
	}
}

func nncsConfigFromEnv() nncsConfig {
	listenIP := parseIPv4Env("NNCS_LISTEN_IP", net.IPv4zero)
	publicIP := parseIPv4Env("NNCS_PUBLIC_IP", nil)
	if publicIP == nil {
		publicIP = parseIPv4Env("NEXTENDO_HOST", net.IPv4(127, 0, 0, 1))
	}

	return nncsConfig{
		ListenIP:        listenIP,
		PublicIP:        publicIP,
		ProbePorts:      parsePortListEnv("NNCS_PROBE_PORTS", []int{nncsDefaultProbePort, nncsDefaultAltPort}),
		SilentPort:      envOrInt("NNCS_SILENT_PORT", nncsDefaultSilentPort),
		FilterProbePort: envOrInt("NNCS_FILTER_PROBE_PORT", nncsDefaultFilterPort),
		ObservationFile: envOr("NNCS_NAT_FILE", "nncs-nat-observations.txt"),
	}
}

func startNNCSResponders(cfg nncsConfig) (*nncsResponders, error) {
	if cfg.ListenIP == nil || cfg.ListenIP.To4() == nil {
		return nil, errors.New("NNCS_LISTEN_IP must be an IPv4 address")
	}
	if cfg.PublicIP == nil || cfg.PublicIP.To4() == nil {
		return nil, errors.New("NNCS_PUBLIC_IP/NEXTENDO_HOST must be an IPv4 address")
	}
	if len(cfg.ProbePorts) == 0 {
		return nil, errors.New("NNCS_PROBE_PORTS must contain at least one port")
	}

	r := &nncsResponders{
		done:   make(chan struct{}),
		writer: &nncsObservationWriter{path: cfg.ObservationFile},
	}

	closeOnError := func(err error) (*nncsResponders, error) {
		r.Close()
		return nil, err
	}

	var filterSocket *net.UDPConn
	var err error
	if cfg.FilterProbePort > 0 {
		filterSocket, err = net.ListenUDP("udp4", &net.UDPAddr{IP: cfg.ListenIP, Port: cfg.FilterProbePort})
		if err != nil {
			return closeOnError(fmt.Errorf("listen %s udp/%d filter probe: %w", cfg.ListenIP, cfg.FilterProbePort, err))
		}
		r.sockets = append(r.sockets, filterSocket)
	}

	for _, port := range cfg.ProbePorts {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: cfg.ListenIP, Port: port})
		if err != nil {
			return closeOnError(fmt.Errorf("listen %s udp/%d probe: %w", cfg.ListenIP, port, err))
		}
		r.sockets = append(r.sockets, conn)
		r.wg.Add(1)
		go r.serveProbe(conn, filterSocket, cfg.PublicIP)
	}

	if cfg.SilentPort > 0 {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: cfg.ListenIP, Port: cfg.SilentPort})
		if err != nil {
			return closeOnError(fmt.Errorf("listen %s udp/%d silent probe: %w", cfg.ListenIP, cfg.SilentPort, err))
		}
		r.sockets = append(r.sockets, conn)
		r.wg.Add(1)
		go r.serveSilent(conn)
	}

	fmt.Printf("[NNCS] listening address=%s probes=%v silent=%d filter-source=%d public=%s observations=%s\n",
		cfg.ListenIP, cfg.ProbePorts, cfg.SilentPort, cfg.FilterProbePort, cfg.PublicIP, cfg.ObservationFile)
	return r, nil
}

func (r *nncsResponders) Close() {
	r.once.Do(func() {
		close(r.done)
		for _, conn := range r.sockets {
			_ = conn.Close()
		}
		r.wg.Wait()
	})
}

func (r *nncsResponders) serveProbe(conn, filterSocket *net.UDPConn, publicIP net.IP) {
	defer r.wg.Done()
	buffer := make([]byte, nncsMaxPacketSize)

	for {
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				fmt.Printf("[NNCS] udp/%d read failed: %v\n", conn.LocalAddr().(*net.UDPAddr).Port, err)
				continue
			}
		}

		response, ok := buildNNCSResponse(buffer[:n], remote, publicIP)
		if !ok {
			nncsCounters.invalidRequests.Add(1)
			continue
		}
		nncsCounters.validRequests.Add(1)

		if _, err := conn.WriteToUDP(response, remote); err != nil {
			nncsCounters.replyErrors.Add(1)
			fmt.Printf("[NNCS] udp/%d reply to %s failed: %v\n", conn.LocalAddr().(*net.UDPAddr).Port, remote, err)
			continue
		}
		nncsCounters.repliesSent.Add(1)

		// Nintendo's filtering test sends an unsolicited datagram from UDP/50920 to the
		// endpoint just observed on UDP/10025. Receipt distinguishes type A from type B.
		filterProbe := "disabled"
		if filterSocket != nil {
			_, _ = filterSocket.WriteToUDP([]byte("Hi"), remote)
			nncsCounters.filterProbesSent.Add(1)
			filterProbe = "sent"
		}
		if err := r.writer.Append(remote); err != nil {
			nncsCounters.observationErrors.Add(1)
			fmt.Printf("[NNCS] observation %s could not be stored: %v\n", remote, err)
		} else {
			nncsCounters.observationsSaved.Add(1)
		}

		fmt.Printf("[NNCS] id=%d observed=%s via %s replied=16 filter-probe=%s\n",
			binary.BigEndian.Uint32(response[0:4]), remote, conn.LocalAddr(), filterProbe)
	}
}

func (r *nncsResponders) serveSilent(conn *net.UDPConn) {
	defer r.wg.Done()
	buffer := make([]byte, nncsMaxPacketSize)

	for {
		_, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				fmt.Printf("[NNCS] udp/%d read failed: %v\n", conn.LocalAddr().(*net.UDPAddr).Port, err)
			}
			continue
		}
		nncsCounters.silentPackets.Add(1)
	}
}

func buildNNCSResponse(request []byte, remote *net.UDPAddr, serverIP net.IP) ([]byte, bool) {
	if len(request) != nncsPacketSize || remote == nil || remote.Port < 1 || remote.Port > 65535 {
		return nil, false
	}

	id := binary.BigEndian.Uint32(request[0:4])
	if id < 101 || id > 103 {
		return nil, false
	}

	observedIP := remote.IP.To4()
	serverIPv4 := serverIP.To4()
	if observedIP == nil || serverIPv4 == nil {
		return nil, false
	}

	response := make([]byte, nncsPacketSize)
	binary.BigEndian.PutUint32(response[0:4], id)
	binary.BigEndian.PutUint32(response[4:8], uint32(remote.Port))
	copy(response[8:12], observedIP)
	copy(response[12:16], serverIPv4)
	return response, true
}

func (w *nncsObservationWriter) Append(remote *net.UDPAddr) error {
	if w == nil || w.path == "" || remote == nil || remote.IP.To4() == nil {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil && filepath.Dir(w.path) != "." {
		return err
	}

	flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	// Keep the append-only handoff bounded. One MiB is thousands of observations and
	// comfortably exceeds the two-minute window consumed by the matchmaking bridge.
	if info, err := os.Stat(w.path); err == nil && info.Size() >= 1<<20 {
		flags = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	}
	file, err := os.OpenFile(w.path, flags, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "%s %d %d\n", remote.IP.To4().String(), remote.Port, time.Now().Unix())
	return err
}

func parseIPv4Env(name string, fallback net.IP) net.IP {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed := net.ParseIP(value)
	if parsed == nil {
		return nil
	}
	return parsed.To4()
}

func parsePortListEnv(name string, fallback []int) []int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return append([]int(nil), fallback...)
	}

	var ports []int
	seen := make(map[int]bool)
	for _, field := range strings.Split(value, ",") {
		port, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}
