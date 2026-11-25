package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	apiMw "github.com/openimsdk/openmeeting-server/internal/api/mw"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/common/token"
	"github.com/openimsdk/protocol/constant"
	pbuser "github.com/openimsdk/protocol/openmeeting/user"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mw"
	"google.golang.org/grpc"
)

type fakeUserClient struct {
	pbuser.UserClient
	storedToken string
	tokenErr    error
	seenOpID    string
}

func (f *fakeUserClient) GetUserToken(ctx context.Context, _ *pbuser.GetUserTokenReq, _ ...grpc.CallOption) (*pbuser.GetUserTokenResp, error) {
	if v, ok := ctx.Value(constant.OperationID).(string); ok {
		f.seenOpID = v
	}
	if f.tokenErr != nil {
		return nil, f.tokenErr
	}
	return &pbuser.GetUserTokenResp{Token: f.storedToken}, nil
}

func TestWsEventsSyntheticOperationIDAndCheckTokenWS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tok := token.New(1, "secret-123")
	t1, err := tok.CreateToken("u-001")
	if err != nil {
		t.Fatalf("CreateToken t1: %v", err)
	}
	t2, err := tok.CreateToken("u-001")
	if err != nil {
		t.Fatalf("CreateToken t2: %v", err)
	}

	fake := &fakeUserClient{storedToken: t1}
	mwApi := apiMw.New(fake, tok)

	r := gin.New()
	r.Use(ensureSyntheticOperationID, mw.GinParseOperationID())
	r.GET("/ws/events", mwApi.CheckTokenWS, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"userID":      c.GetString(constant.OpUserID),
			"operationID": c.GetString(constant.OperationID),
		})
	})

	// 1. Valid token on GET /ws/events without client operationID header -> operationID MUST be bound in gin.Context.
	req := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
	req.Header.Set("token", t1)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid ws token status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if fake.seenOpID == "" {
		t.Fatalf("expected synthetic operationID in context passed to GetUserToken, got empty")
	}

	// 2. Replaced token (second login stored t2) -> HTTP 401 + X-Api-Status-Code: duplicated_login.
	fake.storedToken = t2
	req2 := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
	req2.Header.Set("token", t1)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("kicked ws token status = %d, want 401; body=%s", w2.Code, w2.Body.String())
	}
	if got := w2.Header().Get("X-Api-Status-Code"); got != "duplicated_login" {
		t.Fatalf("X-Api-Status-Code = %q, want duplicated_login", got)
	}

	// 3. Invalid JWT -> HTTP 401 + X-Api-Status-Code: token_invalid.
	req3 := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
	req3.Header.Set("token", "not-a-jwt")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized || w3.Header().Get("X-Api-Status-Code") != "token_invalid" {
		t.Fatalf("invalid jwt got status=%d code=%q, want 401/token_invalid", w3.Code, w3.Header().Get("X-Api-Status-Code"))
	}

	// 4. Token not found in Redis -> HTTP 401 + token_invalid.
	fake.tokenErr = servererrs.ErrUserTokenNotFoundErr.WrapMsg("not found")
	req4 := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
	req4.Header.Set("token", t2)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusUnauthorized || w4.Header().Get("X-Api-Status-Code") != "token_invalid" {
		t.Fatalf("missing redis token got status=%d code=%q, want 401/token_invalid", w4.Code, w4.Header().Get("X-Api-Status-Code"))
	}

	// 5. Transient RPC error -> HTTP 503 so client retries instead of logging out.
	fake.tokenErr = errs.ErrInternalServer.WrapMsg("rpc unavailable")
	req5 := httptest.NewRequest(http.MethodGet, "/ws/events", nil)
	req5.Header.Set("token", t2)
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusServiceUnavailable {
		t.Fatalf("transient rpc error got status=%d, want 503", w5.Code)
	}
}
