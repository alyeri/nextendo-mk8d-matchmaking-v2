package main

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildNNCSResponse(t *testing.T) {
	request := make([]byte, nncsPacketSize)
	binary.BigEndian.PutUint32(request, 101)
	remote := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 49275}

	response, ok := buildNNCSResponse(request, remote, net.IPv4(198, 51, 100, 4))
	if !ok {
		t.Fatal("valid NNCS request was rejected")
	}
	if len(response) != nncsPacketSize {
		t.Fatalf("response length = %d, want %d", len(response), nncsPacketSize)
	}
	if got := binary.BigEndian.Uint32(response[0:4]); got != 101 {
		t.Fatalf("echoed id = %d, want 101", got)
	}
	if got := binary.BigEndian.Uint32(response[4:8]); got != 49275 {
		t.Fatalf("observed port = %d, want 49275", got)
	}
	if got := net.IP(response[8:12]).String(); got != "203.0.113.9" {
		t.Fatalf("observed ip = %s", got)
	}
	if got := net.IP(response[12:16]).String(); got != "198.51.100.4" {
		t.Fatalf("server ip = %s", got)
	}
}

func TestBuildNNCSResponseRejectsUnexpectedTraffic(t *testing.T) {
	cases := [][]byte{
		make([]byte, 15),
		make([]byte, 17),
		make([]byte, 16),
	}
	for i, request := range cases {
		if _, ok := buildNNCSResponse(request, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}, net.IPv4(127, 0, 0, 1)); ok {
			t.Fatalf("case %d was accepted", i)
		}
	}
}

func TestObservationWriterAppendsNatBridgeFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nat.txt")
	writer := &nncsObservationWriter{path: path}
	if err := writer.Append(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 55393}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 3 || fields[0] != "127.0.0.1" || fields[1] != "55393" {
		t.Fatalf("observation = %q", data)
	}
}

func TestParsePortListEnv(t *testing.T) {
	t.Setenv("TEST_NNCS_PORTS", "10025, 10125,10025,bad,70000")
	ports := parsePortListEnv("TEST_NNCS_PORTS", nil)
	if len(ports) != 2 || ports[0] != 10025 || ports[1] != 10125 {
		t.Fatalf("ports = %v", ports)
	}
}
