package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/openmeeting-server/pkg/contactrpc"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
)

type ContactApi struct {
	Client contactrpc.ContactDirectoryServiceClient
}

func NewContactApi(client contactrpc.ContactDirectoryServiceClient) *ContactApi {
	return &ContactApi{Client: client}
}

func opUserID(c *gin.Context) string {
	return strings.TrimSpace(c.GetString(constant.OpUserID))
}

func (a *ContactApi) GetUserProfile(c *gin.Context) {
	var req contactrpc.GetUserProfileReq
	_ = c.ShouldBindJSON(&req)
	req.OperatorUserID = opUserID(c)
	resp, err := a.Client.GetUserProfile(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) UpdateUserProfile(c *gin.Context) {
	var req contactrpc.UpdateUserProfileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.UserID = opUserID(c)
	resp, err := a.Client.UpdateUserProfile(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) SearchDirectory(c *gin.Context) {
	var req contactrpc.SearchDirectoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.OperatorUserID = opUserID(c)
	resp, err := a.Client.SearchDirectory(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) GetOrgTree(c *gin.Context) {
	var req contactrpc.GetOrgTreeReq
	_ = c.ShouldBindJSON(&req)
	req.OperatorUserID = opUserID(c)
	resp, err := a.Client.GetOrgTree(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) GetDeptMembers(c *gin.Context) {
	var req contactrpc.GetDeptMembersReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.OperatorUserID = opUserID(c)
	resp, err := a.Client.GetDeptMembers(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) SyncContactSnapshot(c *gin.Context) {
	var req contactrpc.SyncContactSnapshotReq
	_ = c.ShouldBindJSON(&req)
	req.UserID = opUserID(c)
	resp, err := a.Client.SyncContactSnapshot(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) UpsertContactRelation(c *gin.Context) {
	var req contactrpc.UpsertContactRelationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.OwnerUserID = opUserID(c)
	resp, err := a.Client.UpsertContactRelation(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) RemoveContactRelation(c *gin.Context) {
	var req contactrpc.RemoveContactRelationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.OwnerUserID = opUserID(c)
	resp, err := a.Client.RemoveContactRelation(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) ManageContactGroup(c *gin.Context) {
	var req contactrpc.ManageContactGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.OwnerUserID = opUserID(c)
	resp, err := a.Client.ManageContactGroup(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) SendContactRequest(c *gin.Context) {
	var req contactrpc.SendContactRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.FromUserID = opUserID(c)
	resp, err := a.Client.SendContactRequest(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) HandleContactRequest(c *gin.Context) {
	var req contactrpc.HandleContactRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.OperatorUserID = opUserID(c)
	resp, err := a.Client.HandleContactRequest(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *ContactApi) ListContactRequests(c *gin.Context) {
	var req contactrpc.ListContactRequestsReq
	_ = c.ShouldBindJSON(&req)
	req.UserID = opUserID(c)
	resp, err := a.Client.ListContactRequests(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}
