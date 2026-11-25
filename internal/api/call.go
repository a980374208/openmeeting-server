package api

import (
	"github.com/gin-gonic/gin"
	"github.com/openimsdk/openmeeting-server/pkg/callrpc"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
)

type CallApi struct {
	Client callrpc.CallSignalingServiceClient
}

func NewCallApi(client callrpc.CallSignalingServiceClient) *CallApi {
	return &CallApi{Client: client}
}

func (a *CallApi) StartCall(c *gin.Context) {
	var req callrpc.StartCallReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.CallerUserID = opUserID(c)
	resp, err := a.Client.StartCall(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *CallApi) AcceptCall(c *gin.Context) {
	var req callrpc.AcceptCallReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.CalleeUserID = opUserID(c)
	resp, err := a.Client.AcceptCall(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *CallApi) RejectCall(c *gin.Context) {
	var req callrpc.RejectCallReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.CalleeUserID = opUserID(c)
	resp, err := a.Client.RejectCall(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}

func (a *CallApi) CancelCall(c *gin.Context) {
	var req callrpc.CancelCallReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	req.CallerUserID = opUserID(c)
	resp, err := a.Client.CancelCall(c, &req)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, resp)
}
