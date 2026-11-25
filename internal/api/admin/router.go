package admin

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	adminweb "github.com/openimsdk/openmeeting-server/internal/api/admin/web"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/cache/redis"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/controller"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/database/mgo"
	"github.com/openimsdk/openmeeting-server/pkg/common/token"
	"github.com/openimsdk/openmeeting-server/pkg/events"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	userfind "github.com/openimsdk/openmeeting-server/pkg/user"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mw"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func ensureAdminOperationID(c *gin.Context) {
	opID := strings.TrimSpace(c.GetHeader(constant.OperationID))
	if opID == "" {
		opID = "adm-" + uuid.NewString()[:12]
		c.Request.Header.Set(constant.OperationID, opID)
	}
	c.Set(constant.OperationID, opID)
	c.Next()
}

func adminCorsHandler(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Headers", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Type")
	if c.Request.Method == http.MethodOptions {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	c.Next()
}

func newAdminGinRouter(ctx context.Context, disCov discovery.SvcDiscoveryRegistry, config *Config) (*gin.Engine, error) {
	disCov.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"LoadBalancingPolicy": "%s"}`, "round_robin")))
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), adminCorsHandler)

	// init storage
	mgoCli, err := mongoutil.NewMongoDB(ctx, config.Mongo.Build())
	if err != nil {
		return nil, errs.WrapMsg(err, "admin init mongo failed")
	}
	rdb, err := redisutil.NewRedisClient(ctx, config.Redis.Build())
	if err != nil {
		return nil, errs.WrapMsg(err, "admin init redis failed")
	}

	userDB, err := mgo.NewUserMongo(mgoCli.GetDB())
	if err != nil {
		return nil, errs.WrapMsg(err, "admin init user mongo failed")
	}
	contactDB, err := mgo.NewContactMongo(mgoCli.GetDB())
	if err != nil {
		return nil, errs.WrapMsg(err, "admin init contact mongo failed")
	}
	userCache := redis.NewUser(rdb, userDB, redis.GetDefaultOpt())
	database := controller.NewUser(userDB, userCache, mgoCli.GetTx())

	user := userfind.NewMeeting(disCov, config.Share.RpcRegisterName.User)
	userRpc := rpcclient.NewUser(user)
	adminToken := token.New(config.AdminAPI.Expire, config.AdminAPI.Secret)
	u := NewAdminApi(database, contactDB, rdb, events.NewHub(rdb), *userRpc, adminToken, config.AdminAPI.OrgFounders)

	registerAdminRoutes(r, u)
	return r, nil
}

func registerAdminRoutes(r *gin.Engine, u *ApiAdmin) {
	// Embedded standalone Web Console for server-designated organization founders.
	webFS := adminweb.StaticFS()
	serveIndex := func(c *gin.Context) {
		raw, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "admin console unavailable")
			return
		}
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Data(http.StatusOK, "text/html; charset=utf-8", raw)
	}
	r.GET("/", serveIndex)
	r.GET("/admin/ui", serveIndex)
	r.GET("/admin/ui/*filepath", func(c *gin.Context) {
		rel := strings.TrimPrefix(c.Param("filepath"), "/")
		if rel == "" || rel == "index.html" {
			serveIndex(c)
			return
		}
		if strings.Contains(rel, "..") {
			c.Status(http.StatusNotFound)
			return
		}
		raw, err := fs.ReadFile(webFS, rel)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		ct := "application/octet-stream"
		switch {
		case strings.HasSuffix(rel, ".css"):
			ct = "text/css; charset=utf-8"
		case strings.HasSuffix(rel, ".js"):
			ct = "application/javascript; charset=utf-8"
		case strings.HasSuffix(rel, ".svg"):
			ct = "image/svg+xml"
		case strings.HasSuffix(rel, ".html"):
			ct = "text/html; charset=utf-8"
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, ct, raw)
	})

	adminRouterGroup := r.Group("/admin", ensureAdminOperationID)
	{
		// Public endpoints: founder login + self-service account registration
		adminRouterGroup.POST("/login", u.AdminLogin)
		adminRouterGroup.POST("/user/register", u.RegisterUser)

		// Founder-only endpoints (verified against server-host orgFounders allowlist)
		founderGroup := adminRouterGroup.Group("", u.RequireFounder)
		{
			founderGroup.POST("/logout", u.AdminLogout)
			founderGroup.GET("/me", u.GetMe)
			founderGroup.GET("/org/snapshot", u.GetOrgSnapshot)
			founderGroup.POST("/org/apply", u.ApplyOrgSnapshot)
			founderGroup.POST("/org/upsert_department", u.UpsertDepartment)
			founderGroup.POST("/user/import/json", u.ImportUserByJson)
			founderGroup.POST("/user/import/xlsx", u.ImportUserByXlsx)
		}
	}
}
