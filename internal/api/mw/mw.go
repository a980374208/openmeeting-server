package mw

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	cmConstant "github.com/openimsdk/openmeeting-server/pkg/common/constant"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/common/token"
	"github.com/openimsdk/protocol/constant"
	pbuser "github.com/openimsdk/protocol/openmeeting/user"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

type MW struct {
	client      pbuser.UserClient
	tokenVerify *token.Token
}

func New(c pbuser.UserClient, t *token.Token) *MW {
	return &MW{
		client:      c,
		tokenVerify: t,
	}
}

func (o *MW) parseToken(c *gin.Context) (string, string, error) {
	userToken := c.GetHeader("token")
	if userToken == "" {
		return "", "", errs.ErrArgs.WrapMsg("token is empty")
	}
	userID, err := o.tokenVerify.GetToken(userToken)
	if err != nil {
		return "", "", err
	}
	return userID, userToken, nil
}

func (o *MW) CheckToken(c *gin.Context) {
	userID, userToken, err := o.parseToken(c)
	if err != nil {
		c.Abort()
		apiresp.GinError(c, errs.WrapMsg(err, "parse token failed, invalid token"))
		return
	}
	if err := o.isValidToken(c, userID, userToken); err != nil {
		c.Abort()
		apiresp.GinError(c, errs.WrapMsg(err, "not valid token"))
		return
	}
	o.setToken(c, userID)
}

func isAuthRejectionCode(code int) bool {
	if code >= errs.TokenExpiredError && code <= errs.TokenNotExistError {
		return true
	}
	switch code {
	case servererrs.ArgsError, servererrs.NotFoundUserTokenErr, servererrs.KickOffMeetingError:
		return true
	default:
		return false
	}
}

// CheckTokenWS enforces token validation for WebSocket upgrade routes and returns
// standard HTTP 401/503 responses instead of HTTP 200 JSON so WS clients reliably
// distinguish session kick / token invalidation from transient backend outages.
func (o *MW) CheckTokenWS(c *gin.Context) {
	userID, userToken, err := o.parseToken(c)
	if err != nil {
		c.Header("X-Api-Status-Code", "token_invalid")
		c.Header("X-Api-Message", "invalid or expired token")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"code":    "token_invalid",
			"errCode": errs.TokenInvalidError,
			"message": "invalid or expired token",
		})
		return
	}
	if err := o.isValidToken(c, userID, userToken); err != nil {
		var code int
		if ce, ok := errs.Unwrap(err).(errs.CodeError); ok {
			code = ce.Code()
		}
		if code == servererrs.KickOffMeetingError {
			c.Header("X-Api-Status-Code", "duplicated_login")
			c.Header("X-Api-Message", "session replaced by another login")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "duplicated_login",
				"errCode": servererrs.KickOffMeetingError,
				"message": "session replaced by another login",
			})
			return
		}
		if isAuthRejectionCode(code) {
			c.Header("X-Api-Status-Code", "token_invalid")
			c.Header("X-Api-Message", "invalid or expired token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "token_invalid",
				"errCode": code,
				"message": "invalid or expired token",
			})
			return
		}
		log.ZWarn(c, "ws token validation backend unavailable", err)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"code":    "service_unavailable",
			"message": "authentication service temporarily unavailable",
		})
		return
	}
	o.setToken(c, userID)
}

func (o *MW) isValidToken(c *gin.Context, userID, userToken string) error {
	resp, err := o.client.GetUserToken(c, &pbuser.GetUserTokenReq{UserID: userID})
	if err != nil {
		return err
	}
	if resp.Token == cmConstant.KickOffMeetingMsg {
		return servererrs.ErrKickOffMeeting.WrapMsg("kick off meeting, please login again")
	}
	if resp.Token != "" && resp.Token != userToken {
		log.ZDebug(context.Background(), "token not match", "redis token:", resp.Token, "request token", userToken)
		return servererrs.ErrKickOffMeeting.WrapMsg("kick off meeting for login duplicated, please login again")
	}
	if resp.Token == "" || resp.Token != userToken {
		return errs.ErrTokenExpired.Wrap()
	}

	return nil
}

func (o *MW) setToken(c *gin.Context, userID string) {
	SetToken(c, userID)
}

func SetToken(c *gin.Context, userID string) {
	c.Set(constant.OpUserID, userID)
}
