package nex

import (
	"fmt"
	"time"
)

// Ranking protocol. MK8 uploads its profile blob (UploadCommonData) at online
// startup, and calls two MK8DX-custom methods the base protocol doesn't define.
const (
	ProtocolRanking        uint16 = 0x70
	MethodUploadCommonData uint32 = 0x4

	// MK8DX-custom ranking methods (empty-list responses). 0x16 (worldwide
	// startup) left unhandled was the worldwide-only wall → 2618-0006.
	methodRankingGetCompetitionInfo uint32 = 0x12
	methodRankingWorldwide          uint32 = 0x16
)

// RankingHandler accepts the uploaded common data and stubs the custom methods.
func RankingHandler() RMCHandler {
	return RankingHandlerWithStore(nil)
}

// RankingHandlerWithStore preserves uploaded common data when a store is
// configured. Persistence errors are logged but do not disconnect a player.
func RankingHandlerWithStore(store CommonDataStore) RMCHandler {
	return func(conn *Connection, req *RMCMessage) *RMCMessage {
		switch req.Method {
		case MethodUploadCommonData:
			if store != nil {
				if err := store.PutCommonData(conn.PID, req.Body, time.Now()); err != nil {
					fmt.Printf("[Ranking] common data persistence failed pid=%d: %v\n", conn.PID, err)
				}
			}
			return NewRMCSuccess(conn.Settings, ProtocolRanking, req.Method, req.CallID, nil)
		case methodRankingGetCompetitionInfo, methodRankingWorldwide:
			out := NewStreamOut(conn.Settings)
			out.U32(0) // empty List
			return NewRMCSuccess(conn.Settings, ProtocolRanking, req.Method, req.CallID, out.Bytes())
		default:
			return notImplemented(conn, ProtocolRanking, req)
		}
	}
}
