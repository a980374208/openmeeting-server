package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/openmeeting-server/pkg/agentrpc"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/a2r"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
)

type MeetingApi rpcclient.Meeting

func NewMeetingApi(client rpcclient.Meeting) MeetingApi {
	return MeetingApi(client)
}

func (m *MeetingApi) BookMeeting(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.BookMeeting, m.Client, c)
}

func (m *MeetingApi) CreateImmediateMeeting(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.CreateImmediateMeeting, m.Client, c)
}

func (m *MeetingApi) JoinMeeting(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.JoinMeeting, m.Client, c)
}

func (m *MeetingApi) GetMeetingToken(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.GetMeetingToken, m.Client, c)
}

func (m *MeetingApi) LeaveMeeting(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.LeaveMeeting, m.Client, c)
}

func (m *MeetingApi) EndMeeting(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.EndMeeting, m.Client, c)
}

func (m *MeetingApi) GetMeetings(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.GetMeetings, m.Client, c)
}

func (m *MeetingApi) GetMeeting(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.GetMeeting, m.Client, c)
}

func (m *MeetingApi) GetPersonalMeetingSettings(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.GetPersonalMeetingSettings, m.Client, c)
}

func (m *MeetingApi) ModifyMeetingParticipantNickName(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.ModifyMeetingParticipantNickName, m.Client, c)
}

func (m *MeetingApi) RemoveMeetingParticipants(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.RemoveParticipants, m.Client, c)
}

func (m *MeetingApi) SetMeetingHostInfo(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.SetMeetingHostInfo, m.Client, c)
}

func (m *MeetingApi) SetPersonalMeetingSettings(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.SetPersonalMeetingSettings, m.Client, c)
}

func (m *MeetingApi) UpdateMeeting(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.UpdateMeeting, m.Client, c)
}

func (m *MeetingApi) OperateMeetingAllStream(c *gin.Context) {
	a2r.Call(meeting.MeetingServiceClient.OperateRoomAllStream, m.Client, c)
}

func (m *MeetingApi) RequestAgentToken(c *gin.Context) {
	var req agentrpc.RequestAgentTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	opUserID := strings.TrimSpace(c.GetString(constant.OpUserID))
	if opUserID != "" {
		if req.UserID != "" && req.UserID != opUserID {
			apiresp.GinError(c, servererrs.ErrMeetingAuthCheck.WrapMsg("userID does not match authenticated token user"))
			return
		}
		req.UserID = opUserID
	}
	resp, err := m.AgentClient.RequestAgentToken(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	if resp.Conflict {
		c.JSON(http.StatusOK, gin.H{
			"errCode": servererrs.MeetingAgentAlreadyOnlineError,
			"errMsg":  "An online agent already exists in this meeting room.",
			"data": gin.H{
				"meetingID":     resp.MeetingID,
				"existingAgent": resp.ExistingAgent,
			},
		})
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (m *MeetingApi) GetAgentStatus(c *gin.Context) {
	var req agentrpc.GetAgentStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	if opUserID := strings.TrimSpace(c.GetString(constant.OpUserID)); opUserID != "" && req.UserID == "" {
		req.UserID = opUserID
	}
	resp, err := m.AgentClient.GetAgentStatus(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}
