package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/openimsdk/openmeeting-server/pkg/callrpc"
	"github.com/openimsdk/openmeeting-server/pkg/events"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/log"
	goredis "github.com/redis/go-redis/v9"
)

const (
	maxViewportWatch   = 200
	maxFixedWatch      = 500
	presenceBatchTick  = 200 * time.Millisecond
	writeWait          = 10 * time.Second
	pongWait           = 55 * time.Second
	closeCodeKicked    = 4001
	maxInboundMsgBytes = 64 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type clientFrame struct {
	Type         string   `json:"type"`
	ManualStatus string   `json:"manualStatus,omitempty"`
	FixedUserIDs []string `json:"fixedUserIDs,omitempty"`
	UserIDs      []string `json:"userIDs,omitempty"`
}

type clientConn struct {
	id       string
	userID   string
	token    string
	ws       *websocket.Conn
	sendCh   chan []byte
	closeCh  chan struct{}
	closeOne sync.Once

	watchMu       sync.RWMutex
	fixedWatch    map[string]struct{}
	viewportWatch map[string]struct{}

	presMu        sync.Mutex
	pendingPres   map[string]events.PresenceRecord
	manualState   string
}

func (c *clientConn) closeSignal() {
	c.closeOne.Do(func() { close(c.closeCh) })
}

func (c *clientConn) isWatching(targetUserID string) bool {
	if targetUserID == c.userID {
		return true
	}
	c.watchMu.RLock()
	defer c.watchMu.RUnlock()
	if _, ok := c.fixedWatch[targetUserID]; ok {
		return true
	}
	_, ok := c.viewportWatch[targetUserID]
	return ok
}

func (c *clientConn) queuePresence(rec events.PresenceRecord) {
	c.presMu.Lock()
	if c.pendingPres == nil {
		c.pendingPres = make(map[string]events.PresenceRecord)
	}
	c.pendingPres[rec.UserID] = rec
	c.presMu.Unlock()
}

func (c *clientConn) drainPresence() []events.PresenceRecord {
	c.presMu.Lock()
	defer c.presMu.Unlock()
	if len(c.pendingPres) == 0 {
		return nil
	}
	out := make([]events.PresenceRecord, 0, len(c.pendingPres))
	for _, r := range c.pendingPres {
		out = append(out, r)
	}
	c.pendingPres = make(map[string]events.PresenceRecord)
	return out
}

func (c *clientConn) enqueue(raw []byte) {
	select {
	case c.sendCh <- raw:
	default:
		// Slow consumer; drop oldest or close cleanly on next heartbeat.
	}
}

type Gateway struct {
	gatewayID  string
	rdb        goredis.UniversalClient
	eventHub   *events.Hub
	callClient callrpc.CallSignalingServiceClient

	mu       sync.RWMutex
	conns    map[string]map[string]*clientConn // userID -> connID -> conn
	userSubs map[string]context.CancelFunc     // userID -> cancel per-user Redis subscription
}

func NewGateway(rdb goredis.UniversalClient, callClient callrpc.CallSignalingServiceClient) *Gateway {
	gw := &Gateway{
		gatewayID:  "gw_" + uuid.New().String()[:8],
		rdb:        rdb,
		eventHub:   events.NewHub(rdb),
		callClient: callClient,
		conns:      make(map[string]map[string]*clientConn),
		userSubs:   make(map[string]context.CancelFunc),
	}
	if rdb != nil {
		go gw.runGlobalPubSub()
	}
	return gw
}

func (g *Gateway) runGlobalPubSub() {
	ctx := context.Background()
	sub := g.rdb.Subscribe(ctx, events.PresencePubSubChannel(), events.OrgBroadcastPubSubChannel())
	defer sub.Close()
	ch := sub.Channel()
	for msg := range ch {
		switch msg.Channel {
		case events.PresencePubSubChannel():
			var rec events.PresenceRecord
			if err := json.Unmarshal([]byte(msg.Payload), &rec); err != nil {
				continue
			}
			g.fanoutPresence(rec)
		case events.OrgBroadcastPubSubChannel():
			g.broadcastRaw([]byte(msg.Payload))
		}
	}
}

func (g *Gateway) fanoutPresence(rec events.PresenceRecord) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, userConns := range g.conns {
		for _, conn := range userConns {
			if conn.isWatching(rec.UserID) {
				conn.queuePresence(rec)
			}
		}
	}
}

func (g *Gateway) broadcastRaw(raw []byte) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, userConns := range g.conns {
		for _, conn := range userConns {
			conn.enqueue(raw)
		}
	}
}

func (g *Gateway) ensureUserSubscriptionLocked(userID string) <-chan struct{} {
	if g.rdb == nil {
		return nil
	}
	if _, exists := g.userSubs[userID]; exists {
		return nil
	}
	subCtx, cancel := context.WithCancel(context.Background())
	g.userSubs[userID] = cancel
	ready := make(chan struct{})
	go func() {
		sub := g.rdb.Subscribe(subCtx, events.UserPubSubChannel(userID))
		defer sub.Close()
		// Wait for Redis SUBSCRIBE confirmation before signalling readiness so
		// an immediate second login cannot beat Pub/Sub registration.
		_, _ = sub.ReceiveTimeout(subCtx, 2*time.Second)
		close(ready)
		ch := sub.Channel()
		for {
			select {
			case <-subCtx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				g.deliverToUser(userID, []byte(msg.Payload))
			}
		}
	}()
	return ready
}

func (g *Gateway) deliverToUser(userID string, raw []byte) {
	var env events.Envelope
	_ = json.Unmarshal(raw, &env)

	var kickPayload events.SessionKickedPayload
	isKick := env.Type == events.TypeSessionKicked
	outboundRaw := raw
	if isKick {
		_ = json.Unmarshal(env.Payload, &kickPayload)
		reason := kickPayload.Reason
		if reason == "" {
			reason = "duplicated_login"
		}
		// Never leak keepToken (the newly logged-in device's JWT) to kicked clients.
		sanitizedPayload, _ := json.Marshal(map[string]string{"reason": reason})
		ts := env.TimestampMs
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}
		if clean, err := json.Marshal(events.Envelope{
			Type:        events.TypeSessionKicked,
			TimestampMs: ts,
			Payload:     sanitizedPayload,
		}); err == nil {
			outboundRaw = clean
		}
	}

	g.mu.RLock()
	userConns := make([]*clientConn, 0, len(g.conns[userID]))
	for _, c := range g.conns[userID] {
		userConns = append(userConns, c)
	}
	g.mu.RUnlock()

	for _, c := range userConns {
		if isKick && kickPayload.KeepToken != "" && c.token == kickPayload.KeepToken {
			continue
		}
		c.enqueue(outboundRaw)
		if isKick {
			// Close kicked connection shortly after flushing the kick frame.
			go func(target *clientConn) {
				time.Sleep(150 * time.Millisecond)
				if target.ws != nil {
					_ = target.ws.WriteControl(
						websocket.CloseMessage,
						websocket.FormatCloseMessage(closeCodeKicked, "session_kicked"),
						time.Now().Add(2*time.Second),
					)
				}
				target.closeSignal()
				if target.ws != nil {
					_ = target.ws.Close()
				}
			}(c)
		}
	}
}

func (g *Gateway) registerConn(conn *clientConn) ([]*clientConn, <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.conns[conn.userID] == nil {
		g.conns[conn.userID] = make(map[string]*clientConn)
	}
	// Single-device semantics: collect previous connections for kick or superseded cleanup.
	var stale []*clientConn
	for _, existing := range g.conns[conn.userID] {
		if existing.id != conn.id {
			stale = append(stale, existing)
		}
	}
	g.conns[conn.userID][conn.id] = conn
	ready := g.ensureUserSubscriptionLocked(conn.userID)
	return stale, ready
}

func (g *Gateway) unregisterConn(conn *clientConn) {
	g.mu.Lock()
	if m, ok := g.conns[conn.userID]; ok {
		delete(m, conn.id)
		if len(m) == 0 {
			delete(g.conns, conn.userID)
			if cancel, ok := g.userSubs[conn.userID]; ok {
				cancel()
				delete(g.userSubs, conn.userID)
			}
		}
	}
	g.mu.Unlock()
	if g.eventHub != nil {
		_ = g.eventHub.ClearConnection(context.Background(), conn.userID, conn.id)
	}
}

// HandleEvents handles GET /ws/events?since_seq=<int64> with token authenticated in Header.
func (g *Gateway) HandleEvents(c *gin.Context) {
	userID := strings.TrimSpace(c.GetString(constant.OpUserID))
	if userID == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	userToken := strings.TrimSpace(c.GetHeader("token"))
	sinceSeq, _ := strconv.ParseInt(c.Query("since_seq"), 10, 64)

	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.ZWarn(c, "ws upgrade failed", err)
		return
	}

	conn := &clientConn{
		id:            uuid.New().String()[:12],
		userID:        userID,
		token:         userToken,
		ws:            wsConn,
		sendCh:        make(chan []byte, 128),
		closeCh:       make(chan struct{}),
		fixedWatch:    make(map[string]struct{}),
		viewportWatch: make(map[string]struct{}),
		manualState:   events.PresenceOnline,
	}

	stale, subReady := g.registerConn(conn)
	if subReady != nil {
		select {
		case <-subReady:
		case <-time.After(2 * time.Second):
		}
	}
	for _, old := range stale {
		if old.token != "" && old.token != conn.token {
			kickRaw, _ := json.Marshal(events.Envelope{
				Type:        events.TypeSessionKicked,
				TimestampMs: time.Now().UnixMilli(),
				Payload:     json.RawMessage(`{"reason":"duplicated_login"}`),
			})
			old.enqueue(kickRaw)
			go func(target *clientConn) {
				time.Sleep(100 * time.Millisecond)
				_ = target.ws.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(closeCodeKicked, "duplicated_login"),
					time.Now().Add(time.Second))
				target.closeSignal()
				_ = target.ws.Close()
			}(old)
		} else {
			go func(target *clientConn) {
				_ = target.ws.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "superseded"),
					time.Now().Add(time.Second))
				target.closeSignal()
				_ = target.ws.Close()
			}(old)
		}
	}

	opID := strings.TrimSpace(c.GetString(constant.OperationID))
	if opID == "" {
		opID = "ws-" + conn.id
	}
	ctx := context.WithValue(context.Background(), constant.OperationID, opID)
	selfPres, _ := g.eventHub.SetConnectionOnline(ctx, userID, conn.id, g.gatewayID, events.PresenceOnline)
	missed, latestSeq, resetRequired, _ := g.eventHub.ListSince(ctx, userID, sinceSeq)

	var activeCall *callrpc.CallSessionDTO
	if g.callClient != nil {
		if resp, err := g.callClient.GetActiveIncomingCall(ctx, &callrpc.GetActiveIncomingCallReq{UserID: userID}); err == nil && resp != nil {
			activeCall = resp.Session
		}
	}

	helloPayload, _ := json.Marshal(map[string]any{
		"serverTimeMs":       time.Now().UnixMilli(),
		"latestSeq":          latestSeq,
		"syncResetRequired":  resetRequired,
		"missedEvents":       missed,
		"activeIncomingCall": activeCall,
		"selfPresence":       selfPres,
	})
	helloEnv, _ := json.Marshal(events.Envelope{
		Type:        events.TypeHelloOK,
		Seq:         latestSeq,
		TimestampMs: time.Now().UnixMilli(),
		Payload:     helloPayload,
	})
	conn.enqueue(helloEnv)

	go g.writeLoop(conn)
	g.readLoop(conn)
}

func (g *Gateway) writeLoop(conn *clientConn) {
	ticker := time.NewTicker(presenceBatchTick)
	defer func() {
		ticker.Stop()
		_ = conn.ws.Close()
	}()
	for {
		select {
		case <-conn.closeCh:
			return
		case raw := <-conn.sendCh:
			_ = conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.ws.WriteMessage(websocket.TextMessage, raw); err != nil {
				conn.closeSignal()
				return
			}
		case <-ticker.C:
			batch := conn.drainPresence()
			if len(batch) == 0 {
				continue
			}
			payload, err := json.Marshal(map[string]any{"items": batch})
			if err != nil {
				continue
			}
			raw, err := json.Marshal(events.Envelope{
				Type:        events.TypePresenceChangedBatch,
				TimestampMs: time.Now().UnixMilli(),
				Payload:     payload,
			})
			if err != nil {
				continue
			}
			_ = conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.ws.WriteMessage(websocket.TextMessage, raw); err != nil {
				conn.closeSignal()
				return
			}
		}
	}
}

func (g *Gateway) readLoop(conn *clientConn) {
	defer func() {
		conn.closeSignal()
		g.unregisterConn(conn)
		_ = conn.ws.Close()
	}()
	conn.ws.SetReadLimit(maxInboundMsgBytes)
	_ = conn.ws.SetReadDeadline(time.Now().Add(pongWait))
	conn.ws.SetPongHandler(func(string) error {
		_ = conn.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, data, err := conn.ws.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.ws.SetReadDeadline(time.Now().Add(pongWait))
		var frame clientFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		switch frame.Type {
		case "ping":
			if frame.ManualStatus != "" {
				conn.manualState = frame.ManualStatus
			}
			rec, _, _ := g.eventHub.RefreshConnection(context.Background(), conn.userID, conn.id, conn.manualState)
			payload, _ := json.Marshal(map[string]any{
				"serverTimeMs": time.Now().UnixMilli(),
				"selfPresence": rec,
			})
			pongRaw, _ := json.Marshal(events.Envelope{
				Type:        events.TypePong,
				TimestampMs: time.Now().UnixMilli(),
				Payload:     payload,
			})
			conn.enqueue(pongRaw)
		case "presence.watch":
			g.handlePresenceWatch(conn, frame)
		}
	}
}

func (g *Gateway) handlePresenceWatch(conn *clientConn, frame clientFrame) {
	var queryIDs []string
	conn.watchMu.Lock()
	if frame.FixedUserIDs != nil {
		conn.fixedWatch = make(map[string]struct{}, len(frame.FixedUserIDs))
		for _, id := range frame.FixedUserIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			conn.fixedWatch[id] = struct{}{}
			queryIDs = append(queryIDs, id)
			if len(conn.fixedWatch) >= maxFixedWatch {
				break
			}
		}
	}
	if frame.UserIDs != nil {
		conn.viewportWatch = make(map[string]struct{}, len(frame.UserIDs))
		for _, id := range frame.UserIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			conn.viewportWatch[id] = struct{}{}
			queryIDs = append(queryIDs, id)
			if len(conn.viewportWatch) >= maxViewportWatch {
				break
			}
		}
	}
	conn.watchMu.Unlock()

	if len(queryIDs) > 0 && g.eventHub != nil {
		batch, err := g.eventHub.GetPresenceBatch(context.Background(), queryIDs)
		if err == nil {
			for _, rec := range batch {
				conn.queuePresence(rec)
			}
		}
	}
}
