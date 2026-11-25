package api

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	apiMw "github.com/openimsdk/openmeeting-server/internal/api/mw"
	apiWs "github.com/openimsdk/openmeeting-server/internal/api/ws"
	"github.com/openimsdk/openmeeting-server/pkg/common/token"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	"github.com/openimsdk/openmeeting-server/pkg/user"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mw"
	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Keep "" in whitelist so generic openim GinParseToken stays bypassed;
// every protected route explicitly enforces mwApi.CheckToken, while /user/login,
// /user/register, and /webhook/livekit handle their own authentication.
var whitelist = []string{
	"",
}

func ensureSyntheticOperationID(c *gin.Context) {
	path := c.Request.URL.Path
	if path == "/webhook/livekit" || path == "/ws/events" {
		opID := c.GetHeader(constant.OperationID)
		if opID == "" {
			opID = "srv-" + uuid.NewString()[:12]
			c.Request.Header.Set(constant.OperationID, opID)
		}
		// mw.GinParseOperationID only populates constant.OperationID for POST requests;
		// explicitly set it on gin.Context so GET /ws/events passes gRPC client interceptors.
		c.Set(constant.OperationID, opID)
	}
	c.Next()
}

func secretKey(secret string) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	}
}

func newGinRouter(disCov discovery.SvcDiscoveryRegistry, config *Config) *gin.Engine {
	disCov.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"LoadBalancingPolicy": "%s"}`, "round_robin")))
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), mw.CorsHandler(), ensureSyntheticOperationID, mw.GinParseOperationID(), mw.GinParseToken(secretKey(config.API.Secret), whitelist))
	// init rpc client here
	userRpc := user.NewMeetingUserClient(disCov, config.Share.RpcRegisterName.User)
	contactRpc := user.NewContactDirectoryClient(disCov, config.Share.RpcRegisterName.User)
	meetingRpc := rpcclient.NewMeeting(disCov, config.Share.RpcRegisterName.Meeting)

	var rdb goredis.UniversalClient
	if len(config.Redis.Address) > 0 {
		var err error
		rdb, err = redisutil.NewRedisClient(context.Background(), config.Redis.Build())
		if err != nil {
			log.ZWarn(context.Background(), "api redis init failed; ws events degraded", err)
		}
	}

	userToken := token.New(config.API.Expire, config.API.Secret)
	mwApi := apiMw.New(userRpc, userToken)
	u := NewUserApi(userRpc)
	ct := NewContactApi(contactRpc)
	userRouterGroup := r.Group("/user")
	{
		userRouterGroup.POST("/register", u.UserRegister)
		userRouterGroup.POST("/login", u.UserLogin)
		userRouterGroup.POST("/get_users_info", mwApi.CheckToken, u.GetUsersPublicInfo)
		userRouterGroup.POST("/update_user_password", mwApi.CheckToken, u.UpdateUserPassword)
		userRouterGroup.POST("/logout", mwApi.CheckToken, u.UserLogout)
		userRouterGroup.POST("/get_profile", mwApi.CheckToken, ct.GetUserProfile)
		userRouterGroup.POST("/update_profile", mwApi.CheckToken, ct.UpdateUserProfile)
	}

	dirRouterGroup := r.Group("/directory", mwApi.CheckToken)
	{
		dirRouterGroup.POST("/search", ct.SearchDirectory)
		dirRouterGroup.POST("/get_org_tree", ct.GetOrgTree)
		dirRouterGroup.POST("/get_dept_members", ct.GetDeptMembers)
	}

	contactRouterGroup := r.Group("/contact", mwApi.CheckToken)
	{
		contactRouterGroup.POST("/sync_snapshot", ct.SyncContactSnapshot)
		contactRouterGroup.POST("/upsert_relation", ct.UpsertContactRelation)
		contactRouterGroup.POST("/remove_relation", ct.RemoveContactRelation)
		contactRouterGroup.POST("/manage_group", ct.ManageContactGroup)
		contactRouterGroup.POST("/send_request", ct.SendContactRequest)
		contactRouterGroup.POST("/handle_request", ct.HandleContactRequest)
		contactRouterGroup.POST("/list_requests", ct.ListContactRequests)
	}

	callApi := NewCallApi(meetingRpc.CallClient)
	callRouterGroup := r.Group("/call", mwApi.CheckToken)
	{
		callRouterGroup.POST("/start", callApi.StartCall)
		callRouterGroup.POST("/accept", callApi.AcceptCall)
		callRouterGroup.POST("/reject", callApi.RejectCall)
		callRouterGroup.POST("/cancel", callApi.CancelCall)
	}

	wsGateway := apiWs.NewGateway(rdb, meetingRpc.CallClient)
	r.GET("/ws/events", mwApi.CheckTokenWS, wsGateway.HandleEvents)

	lkWebhook := NewLiveKitWebhookApi(config.Rtc.ApiKey, config.Rtc.ApiSecret, meetingRpc.CallClient)
	r.POST("/webhook/livekit", lkWebhook.HandleWebhook)

	m := NewMeetingApi(*meetingRpc)
	meetingRouterGroup := r.Group("/meeting")
	{
		meetingRouterGroup.POST("/book_meeting", mwApi.CheckToken, m.BookMeeting)
		meetingRouterGroup.POST("/create_immediate_meeting", mwApi.CheckToken, m.CreateImmediateMeeting)
		meetingRouterGroup.POST("/join_meeting", mwApi.CheckToken, m.JoinMeeting)
		meetingRouterGroup.POST("/get_meeting_token", mwApi.CheckToken, m.GetMeetingToken)
		meetingRouterGroup.POST("/request_agent_token", mwApi.CheckToken, m.RequestAgentToken)
		meetingRouterGroup.POST("/get_agent_status", mwApi.CheckToken, m.GetAgentStatus)

		meetingRouterGroup.POST("/update_meeting", mwApi.CheckToken, m.UpdateMeeting)
		meetingRouterGroup.POST("/get_meeting", mwApi.CheckToken, m.GetMeeting)
		meetingRouterGroup.POST("/get_meetings", mwApi.CheckToken, m.GetMeetings)
		meetingRouterGroup.POST("/leave_meeting", mwApi.CheckToken, m.LeaveMeeting)
		meetingRouterGroup.POST("/end_meeting", mwApi.CheckToken, m.EndMeeting)
		meetingRouterGroup.POST("/set_personal_setting", mwApi.CheckToken, m.SetPersonalMeetingSettings)
		meetingRouterGroup.POST("/get_personal_setting", mwApi.CheckToken, m.GetPersonalMeetingSettings)
		meetingRouterGroup.POST("/operate_meeting_all_stream", mwApi.CheckToken, m.OperateMeetingAllStream)
		meetingRouterGroup.POST("/modify_meeting_participant_name", mwApi.CheckToken, m.ModifyMeetingParticipantNickName)
		meetingRouterGroup.POST("/remove_participants", mwApi.CheckToken, m.RemoveMeetingParticipants)
		meetingRouterGroup.POST("/set_meeting_host_info", mwApi.CheckToken, m.SetMeetingHostInfo)
	}
	return r
}
