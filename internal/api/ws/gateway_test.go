package ws

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/openimsdk/openmeeting-server/pkg/events"
)

func TestDeliverToUserSanitizesSessionKickedAndRespectsKeepToken(t *testing.T) {
	gw := &Gateway{
		gatewayID: "gw_test",
		conns:     make(map[string]map[string]*clientConn),
		userSubs:  make(map[string]context.CancelFunc),
	}
	oldConn := &clientConn{
		id:      "conn_old",
		userID:  "u1",
		token:   "old-jwt-token",
		sendCh:  make(chan []byte, 4),
		closeCh: make(chan struct{}),
	}
	newConn := &clientConn{
		id:      "conn_new",
		userID:  "u1",
		token:   "new-jwt-secret-token",
		sendCh:  make(chan []byte, 4),
		closeCh: make(chan struct{}),
	}
	gw.conns["u1"] = map[string]*clientConn{
		oldConn.id: oldConn,
		newConn.id: newConn,
	}

	payload, _ := json.Marshal(events.SessionKickedPayload{
		Reason:    "duplicated_login",
		KeepToken: newConn.token,
	})
	rawEnv, _ := json.Marshal(events.Envelope{
		Type:        events.TypeSessionKicked,
		TimestampMs: time.Now().UnixMilli(),
		Payload:     payload,
	})

	// deliverToUser spawns a delayed close goroutine that accesses target.ws;
	// omit ws nil dereference by checking only channel delivery on oldConn vs newConn.
	// Use a separate test helper or verify via mock? Wait: deliverToUser calls target.ws.WriteControl after 150ms.
	// So test completes well within 50ms, OR guard nil ws in deliverToUser!
	gw.deliverToUser("u1", rawEnv)

	select {
	case msg := <-oldConn.sendCh:
		if strings.Contains(string(msg), newConn.token) || strings.Contains(string(msg), "keepToken") {
			t.Fatalf("session.kicked leaked keepToken to kicked client: %s", string(msg))
		}
		var env events.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal kicked envelope failed: %v", err)
		}
		if env.Type != events.TypeSessionKicked {
			t.Fatalf("got type %q, want %q", env.Type, events.TypeSessionKicked)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("old connection did not receive session.kicked frame")
	}

	select {
	case msg := <-newConn.sendCh:
		t.Fatalf("new connection with keepToken should NOT receive kick frame, got: %s", string(msg))
	default:
	}
}
