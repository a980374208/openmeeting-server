package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/openmeeting-server/pkg/agentrpc"
	"github.com/openimsdk/openmeeting-server/pkg/callrpc"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/a2r"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"google.golang.org/protobuf/encoding/protojson"
)

type MeetingApi rpcclient.Meeting

func NewMeetingApi(client rpcclient.Meeting) MeetingApi {
	return MeetingApi(client)
}

func (m *MeetingApi) BookMeeting(c *gin.Context) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("read body failed"))
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

	var ext struct {
		InviteeUserIDs []string `json:"inviteeUserIDs"`
	}
	_ = json.Unmarshal(rawBody, &ext)

	var req meeting.BookMeetingReq
	if err := json.Unmarshal(rawBody, &req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	if op := opUserID(c); op != "" && req.CreatorUserID == "" {
		req.CreatorUserID = op
	}
	resp, err := m.Client.BookMeeting(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	var savedInvitees []string
	if len(ext.InviteeUserIDs) > 0 && m.CallClient != nil && resp.Detail != nil &&
		resp.Detail.Info != nil && resp.Detail.Info.SystemGenerated != nil {
		meetingID := resp.Detail.Info.SystemGenerated.MeetingID
		title := ""
		var sched int64
		if resp.Detail.Info.CreatorDefinedMeeting != nil {
			title = resp.Detail.Info.CreatorDefinedMeeting.Title
			sched = resp.Detail.Info.CreatorDefinedMeeting.ScheduledTime
		}
		if iResp, iErr := m.CallClient.SetMeetingInvitees(c, &callrpc.SetMeetingInviteesReq{
			MeetingID:      meetingID,
			OperatorUserID: opUserID(c),
			InviteeUserIDs: ext.InviteeUserIDs,
			MeetingTitle:   title,
			ScheduledTime:  sched,
		}); iErr == nil && iResp != nil {
			savedInvitees = iResp.InviteeUserIDs
		}
	}
	dataMap, err := protoMessageToMap(resp)
	if err != nil {
		apiresp.GinSuccess(c, resp)
		return
	}
	if len(savedInvitees) > 0 {
		if detailMap, ok := dataMap["detail"].(map[string]any); ok {
			detailMap["inviteeUserIDs"] = savedInvitees
		}
	}
	apiresp.GinSuccess(c, dataMap)
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
	req, err := a2r.ParseRequest[meeting.GetMeetingsReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	resp, err := m.Client.GetMeetings(c, req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	dataMap, err := protoMessageToMap(resp)
	if err != nil || m.CallClient == nil {
		apiresp.GinSuccess(c, resp)
		return
	}
	if details, ok := dataMap["meetingDetails"].([]any); ok && len(details) > 0 {
		ids := make([]string, 0, len(details))
		for _, item := range details {
			if id := extractMeetingIDFromMap(item); id != "" {
				ids = append(ids, id)
			}
		}
		if invResp, iErr := m.CallClient.GetMeetingInviteesBatch(c, &callrpc.GetMeetingInviteesBatchReq{MeetingIDs: ids}); iErr == nil && invResp != nil {
			for _, item := range details {
				if mObj, ok := item.(map[string]any); ok {
					if id := extractMeetingIDFromMap(mObj); id != "" {
						if list := invResp.Invitees[id]; len(list) > 0 {
							mObj["inviteeUserIDs"] = list
						}
					}
				}
			}
		}
	}
	apiresp.GinSuccess(c, dataMap)
}

func (m *MeetingApi) GetMeeting(c *gin.Context) {
	req, err := a2r.ParseRequest[meeting.GetMeetingReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	resp, err := m.Client.GetMeeting(c, req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	dataMap, err := protoMessageToMap(resp)
	if err != nil || m.CallClient == nil {
		apiresp.GinSuccess(c, resp)
		return
	}
	if detailObj, ok := dataMap["meetingDetail"].(map[string]any); ok {
		if id := extractMeetingIDFromMap(detailObj); id != "" {
			if invResp, iErr := m.CallClient.GetMeetingInviteesBatch(c, &callrpc.GetMeetingInviteesBatchReq{MeetingIDs: []string{id}}); iErr == nil && invResp != nil {
				if list := invResp.Invitees[id]; len(list) > 0 {
					detailObj["inviteeUserIDs"] = list
				}
			}
		}
	}
	apiresp.GinSuccess(c, dataMap)
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
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("read body failed"))
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

	var ext struct {
		MeetingID      string   `json:"meetingID"`
		InviteeUserIDs []string `json:"inviteeUserIDs"`
	}
	var rawMap map[string]json.RawMessage
	_ = json.Unmarshal(rawBody, &rawMap)
	_ = json.Unmarshal(rawBody, &ext)
	_, hasInvitees := rawMap["inviteeUserIDs"]

	var req meeting.UpdateMeetingRequest
	unmarshaler := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := unmarshaler.Unmarshal(rawBody, &req); err != nil {
		// Fallback to standard JSON for wrapper fields.
		if jErr := json.Unmarshal(rawBody, &req); jErr != nil {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg(jErr.Error()))
			return
		}
	}
	resp, err := m.Client.UpdateMeeting(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	if hasInvitees && ext.MeetingID != "" && m.CallClient != nil {
		_, _ = m.CallClient.SetMeetingInvitees(c, &callrpc.SetMeetingInviteesReq{
			MeetingID:      ext.MeetingID,
			OperatorUserID: opUserID(c),
			InviteeUserIDs: ext.InviteeUserIDs,
		})
	}
	apiresp.GinSuccess(c, resp)
}

func protoMessageToMap(msg any) (map[string]any, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func extractMeetingIDFromMap(detail any) string {
	mObj, ok := detail.(map[string]any)
	if !ok {
		return ""
	}
	info, ok := mObj["info"].(map[string]any)
	if !ok {
		return ""
	}
	sys, ok := info["systemGenerated"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := sys["meetingID"].(string)
	return id
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
