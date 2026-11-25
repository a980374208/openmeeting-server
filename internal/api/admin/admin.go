package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/openmeeting-server/pkg/apistruct"
	"github.com/openimsdk/openmeeting-server/pkg/common/securetools"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/controller"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/database"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/openmeeting-server/pkg/common/token"
	"github.com/openimsdk/openmeeting-server/pkg/common/xlsx"
	"github.com/openimsdk/openmeeting-server/pkg/common/xlsx/definition"
	"github.com/openimsdk/openmeeting-server/pkg/events"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/protocol/openmeeting/admin"
	"github.com/openimsdk/tools/a2r"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	goredis "github.com/redis/go-redis/v9"
	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	founderContextKey = "admin_founder_user"
	adminTokenPrefix  = "om:admin:token:"
)

type ApiAdmin struct {
	client             rpcclient.User
	userStorageHandler controller.User
	contactDB          database.Contact
	rdb                goredis.UniversalClient
	eventHub           *events.Hub
	config             *Config
	tokenVerify        *token.Token
	founders           map[string]struct{}
}

func buildFounderSet(configured []string, envRaw string) map[string]struct{} {
	set := make(map[string]struct{})
	add := func(raw string) {
		v := strings.TrimSpace(raw)
		if v != "" {
			set[v] = struct{}{}
		}
	}
	for _, f := range configured {
		add(f)
	}
	for _, part := range strings.Split(envRaw, ",") {
		add(part)
	}
	return set
}

func NewAdminApi(
	userStorage controller.User,
	contactDB database.Contact,
	rdb goredis.UniversalClient,
	eventHub *events.Hub,
	client rpcclient.User,
	t *token.Token,
	configuredFounders []string,
) *ApiAdmin {
	return &ApiAdmin{
		client:             client,
		userStorageHandler: userStorage,
		contactDB:          contactDB,
		rdb:                rdb,
		eventHub:           eventHub,
		tokenVerify:        t,
		founders:           buildFounderSet(configuredFounders, os.Getenv("OPENMEETING_ORG_FOUNDERS")),
	}
}

func (a *ApiAdmin) isFounderUser(u *model.User) bool {
	if u == nil || len(a.founders) == 0 {
		return false
	}
	if _, ok := a.founders[strings.TrimSpace(u.Account)]; ok {
		return true
	}
	if _, ok := a.founders[strings.TrimSpace(u.UserID)]; ok {
		return true
	}
	return false
}

func adminTokenRedisKey(userID string) string {
	return adminTokenPrefix + userID
}

func (a *ApiAdmin) storeAdminToken(ctx context.Context, userID, tok string) error {
	if a.rdb == nil {
		return nil
	}
	ttl := 24 * time.Hour
	if a.tokenVerify != nil && a.tokenVerify.Expires > 0 {
		ttl = a.tokenVerify.Expires
	}
	return a.rdb.Set(ctx, adminTokenRedisKey(userID), tok, ttl).Err()
}

func (a *ApiAdmin) verifyAdminTokenStored(ctx context.Context, userID, tok string) bool {
	if a.rdb == nil {
		return true
	}
	stored, err := a.rdb.Get(ctx, adminTokenRedisKey(userID)).Result()
	if err != nil {
		return false
	}
	return stored == tok
}

func (a *ApiAdmin) clearAdminToken(ctx context.Context, userID string) {
	if a.rdb == nil || userID == "" {
		return
	}
	_ = a.rdb.Del(ctx, adminTokenRedisKey(userID)).Err()
}

// RequireFounder authenticates the admin JWT and enforces server-host founder allowlist.
func (a *ApiAdmin) RequireFounder(c *gin.Context) {
	if len(a.founders) == 0 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"errCode": errs.NoPermissionError,
			"errMsg":  "no organization founders configured on server host",
		})
		return
	}
	rawTok := strings.TrimSpace(c.GetHeader("token"))
	if rawTok == "" {
		auth := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			rawTok = strings.TrimSpace(auth[7:])
		}
	}
	if rawTok == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"errCode": errs.TokenInvalidError,
			"errMsg":  "missing admin token",
		})
		return
	}
	userID, err := a.tokenVerify.GetToken(rawTok)
	if err != nil || strings.TrimSpace(userID) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"errCode": errs.TokenInvalidError,
			"errMsg":  "invalid or expired admin token",
		})
		return
	}
	if !a.verifyAdminTokenStored(c, userID, rawTok) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"errCode": errs.TokenInvalidError,
			"errMsg":  "admin session expired, please sign in again",
		})
		return
	}
	users, err := a.userStorageHandler.FindByUserIDs(c, []string{userID})
	if err != nil || len(users) == 0 || users[0] == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"errCode": errs.TokenInvalidError,
			"errMsg":  "founder account not found",
		})
		return
	}
	if !a.isFounderUser(users[0]) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"errCode": errs.NoPermissionError,
			"errMsg":  "only server-designated organization founders can perform this action",
		})
		return
	}
	c.Set(constant.OpUserID, users[0].UserID)
	c.Set(founderContextKey, users[0])
	c.Next()
}

func (a *ApiAdmin) AdminLogin(c *gin.Context) {
	if len(a.founders) == 0 {
		apiresp.GinError(c, errs.ErrNoPermission.WrapMsg("no organization founders configured on server host (set orgFounders in openmeeting-admin-api.yml)"))
		return
	}
	req, err := a2r.ParseRequest[admin.UserLoginReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	user, err := a.userStorageHandler.GetByAccount(c, strings.TrimSpace(req.Account))
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "login failed, not found account, please check"))
		return
	}
	if !a.isFounderUser(user) {
		apiresp.GinError(c, errs.ErrNoPermission.WrapMsg("only server-designated organization founders can log in to the organization console"))
		return
	}
	saltPasswd := securetools.VerifyPassword(req.Password, user.SaltValue)
	if saltPasswd != user.Password {
		apiresp.GinError(c, errs.WrapMsg(err, "wrong password or user account"))
		return
	}
	adminToken, err := a.tokenVerify.CreateToken(user.UserID)
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "create token failed, please check"))
		return
	}
	// Store under isolated admin Redis namespace so desktop client login tokens are never overwritten.
	if err := a.storeAdminToken(c, user.UserID, adminToken); err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "store admin session failed"))
		return
	}
	apiresp.GinSuccess(c, &apistruct.AdminLoginResp{
		AdminAccount: user.Account,
		AdminToken:   adminToken,
		Nickname:     user.Nickname,
	})
}

func (a *ApiAdmin) AdminLogout(c *gin.Context) {
	userID := strings.TrimSpace(c.GetString(constant.OpUserID))
	a.clearAdminToken(c, userID)
	apiresp.GinSuccess(c, gin.H{"ok": true})
}

func (a *ApiAdmin) GetMe(c *gin.Context) {
	raw, ok := c.Get(founderContextKey)
	if !ok {
		apiresp.GinError(c, errs.ErrNoPermission.WrapMsg("founder context missing"))
		return
	}
	u, _ := raw.(*model.User)
	apiresp.GinSuccess(c, gin.H{
		"userID":        u.UserID,
		"account":       u.Account,
		"nickname":      u.Nickname,
		"foundersCount": len(a.founders),
	})
}

func (a *ApiAdmin) ImportUserByJson(c *gin.Context) {
	formFile, err := c.FormFile("data")
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "get form file failed"))
		return
	}

	// open file
	f, err := formFile.Open()
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "open file failed"))
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
		}
	}()

	var users []definition.User
	if err := json.NewDecoder(f).Decode(&users); err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "decode json failed"))
		return
	}

	if err := a.importUsersWithOrg(c, users); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("import users failed: "+err.Error()))
		return
	}
	apiresp.GinSuccess(c, nil)
}

func (a *ApiAdmin) ImportUserByXlsx(c *gin.Context) {
	formFile, _, err := c.Request.FormFile("data")
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "get form file failed"))
		return
	}

	file, err := excelize.OpenReader(formFile)
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "open failed"))
		return
	}

	// read sheet
	rows, err := file.GetRows("Sheet1")
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "get rows failed"))
		return
	}
	if len(rows) < 2 {
		apiresp.GinSuccess(c, nil)
		return
	}

	// get header
	headers := rows[0]
	colIndex := xlsx.GetColumnIndex(headers)

	// get data
	var users []definition.User
	for _, row := range rows[1:] { // skip header
		user := definition.User{}
		if err := xlsx.SetStructValues(&user, row, colIndex); err != nil {
			apiresp.GinError(c, errs.WrapMsg(err, "please check xlsx structure"))
			return
		}
		users = append(users, user)
	}
	if err := a.importUsersWithOrg(c, users); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("import users failed: "+err.Error()))
		return
	}
	apiresp.GinSuccess(c, nil)
}

func (a *ApiAdmin) importUsersWithOrg(c *gin.Context, users []definition.User) error {
	orgTouched := false
	for _, user := range users {
		account := strings.TrimSpace(user.Account)
		if account == "" {
			continue
		}
		var userID string
		existing, err := a.userStorageHandler.GetByAccount(c, account)
		if err == nil && existing != nil {
			userID = existing.UserID
		} else {
			userID, err = a.userStorageHandler.GenerateUserID(c)
			if err != nil {
				return errs.WrapMsg(err, "generate user id failed")
			}
		}
		var passwd, salt string
		if strings.TrimSpace(user.Password) != "" {
			passwd, salt = securetools.HashPassword(user.Password)
		}
		var deptIDs []string
		var deptNames []string
		if a.contactDB != nil && strings.TrimSpace(user.Department) != "" {
			depts, err := a.contactDB.EnsureDepartmentPath(c, user.Department)
			if err != nil {
				return errs.WrapMsg(err, "ensure department path failed")
			}
			if len(depts) > 0 {
				orgTouched = true
				leaf := depts[len(depts)-1]
				deptIDs = []string{leaf.DeptID}
				for _, d := range depts {
					deptNames = append(deptNames, d.Name)
				}
			}
		}
		wasInOrg := existing != nil && len(existing.PrimaryDeptIDs) > 0
		empNo := ""
		if existing != nil {
			empNo = strings.TrimSpace(existing.EmployeeNo)
		}
		allocatedNewEmpNo := false
		if a.contactDB != nil && len(deptIDs) > 0 && (!wasInOrg || empNo == "") {
			if allocated, allocErr := a.contactDB.AllocateEmployeeNo(c); allocErr == nil && allocated != "" {
				empNo = allocated
				allocatedNewEmpNo = true
			}
		}
		dbUser := &model.User{
			UserID:         userID,
			Nickname:       strings.TrimSpace(user.Nickname),
			Account:        account,
			Password:       passwd,
			SaltValue:      salt,
			EmployeeNo:     empNo,
			Email:          strings.TrimSpace(user.Email),
			Phone:          strings.TrimSpace(user.Phone),
			JobTitle:       strings.TrimSpace(user.JobTitle),
			Signature:      strings.TrimSpace(user.Signature),
			AvatarURL:      strings.TrimSpace(user.AvatarURL),
			PrimaryDeptIDs: deptIDs,
			Status:         model.UserStatusActive,
		}
		dbUser.SearchTokens = model.BuildSearchTokens(dbUser, deptNames...)
		if err := a.userStorageHandler.UpsertByAccount(c, dbUser); err != nil {
			return err
		}
		if a.contactDB != nil && len(deptIDs) > 0 {
			isLeader := strings.EqualFold(strings.TrimSpace(user.IsLeader), "true") || strings.TrimSpace(user.IsLeader) == "1"
			if err := a.contactDB.SetUserDepartments(c, userID, deptIDs, isLeader); err != nil {
				return err
			}
			if allocatedNewEmpNo {
				orgCode, _ := a.contactDB.EnsureOrgCode(c)
				_ = a.contactDB.CreateEmployeeRecord(c, &model.OrgEmployeeRecord{
					OrgCode:    orgCode,
					EmployeeNo: empNo,
					UserID:     userID,
					Account:    account,
					Nickname:   dbUser.Nickname,
					JobTitle:   dbUser.JobTitle,
					DeptNames:  deptNames,
					Status:     model.OrgEmployeeStatusActive,
					JoinedAt:   time.Now().UnixMilli(),
				})
			}
		}
	}
	if orgTouched {
		ver := time.Now().UnixMilli()
		if a.contactDB != nil {
			if bumped, err := a.contactDB.BumpOrgTreeVersion(c, ver); err == nil && bumped > 0 {
				ver = bumped
			}
		}
		if a.eventHub != nil {
			_ = a.eventHub.PublishOrgTreeUpdated(c, ver)
		}
	}
	return nil
}

func (a *ApiAdmin) UpsertDepartment(c *gin.Context) {
	var req struct {
		Path      string `json:"path"`
		SortOrder int32  `json:"sortOrder"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("department path is required"))
		return
	}
	if a.contactDB == nil {
		apiresp.GinError(c, errs.ErrInternalServer.WrapMsg("contact storage unavailable"))
		return
	}
	depts, err := a.contactDB.EnsureDepartmentPath(c, req.Path)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	if len(depts) > 0 && req.SortOrder != 0 {
		leaf := depts[len(depts)-1]
		leaf.SortOrder = req.SortOrder
		_ = a.contactDB.UpsertDepartment(c, leaf)
	}
	ver, _ := a.contactDB.BumpOrgTreeVersion(c, time.Now().UnixMilli())
	if a.eventHub != nil {
		_ = a.eventHub.PublishOrgTreeUpdated(c, ver)
	}
	apiresp.GinSuccess(c, gin.H{"departments": depts, "treeVersion": ver})
}

func (a *ApiAdmin) RegisterUser(c *gin.Context) {
	req, err := a2r.ParseRequest[admin.UserRegisterReq](c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	account := strings.TrimSpace(req.Account)
	if account == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("account is required"))
		return
	}
	if existing, err := a.userStorageHandler.GetByAccount(c, account); err == nil && existing != nil {
		apiresp.GinError(c, servererrs.ErrRegisteredAlready.WrapMsg("account already registered"))
		return
	}
	userID, err := a.userStorageHandler.GenerateUserID(c)
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "generate user id failed"))
		return
	}
	passwd, salt := securetools.HashPassword(req.Password)
	dbUser := &model.User{
		UserID:    userID,
		Nickname:  strings.TrimSpace(req.Nickname),
		Account:   account,
		Password:  passwd,
		SaltValue: salt,
		Status:    model.UserStatusActive,
	}
	dbUser.SearchTokens = model.BuildSearchTokens(dbUser)
	if err := a.userStorageHandler.Create(c, []*model.User{dbUser}); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			apiresp.GinError(c, servererrs.ErrRegisteredAlready.WrapMsg("account already registered"))
			return
		}
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("create users failed "+err.Error()))
		return
	}
	apiresp.GinSuccess(c, nil)
}
