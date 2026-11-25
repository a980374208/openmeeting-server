package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"
	"github.com/openimsdk/openmeeting-server/pkg/callrpc"
	"github.com/openimsdk/tools/log"
)

type LiveKitWebhookApi struct {
	keyProvider auth.KeyProvider
	callClient  callrpc.CallSignalingServiceClient
}

func NewLiveKitWebhookApi(apiKey, apiSecret string, callClient callrpc.CallSignalingServiceClient) *LiveKitWebhookApi {
	var kp auth.KeyProvider
	if apiKey != "" && apiSecret != "" {
		kp = auth.NewSimpleKeyProvider(apiKey, apiSecret)
	}
	return &LiveKitWebhookApi{
		keyProvider: kp,
		callClient:  callClient,
	}
}

// HandleWebhook validates the LiveKit Authorization JWT signature and forwards
// participant/room lifecycle transitions to CallSignalingService for presence sync.
func (w *LiveKitWebhookApi) HandleWebhook(c *gin.Context) {
	if w.keyProvider == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	event, err := webhook.ReceiveWebhookEvent(c.Request, w.keyProvider)
	if err != nil {
		log.ZWarn(c, "livekit webhook signature verification failed", err)
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	req := &callrpc.LiveKitWebhookEventReq{
		Event: event.GetEvent(),
	}
	if event.GetRoom() != nil {
		req.RoomName = event.GetRoom().GetName()
	}
	if event.GetParticipant() != nil {
		req.ParticipantIdentity = event.GetParticipant().GetIdentity()
	}
	if w.callClient != nil {
		if _, err := w.callClient.HandleLiveKitWebhookEvent(c, req); err != nil {
			log.ZError(c, "handle livekit webhook event failed", err, "event", req.Event)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
	}
	c.Status(http.StatusOK)
}
