package admin

import (
	"database/sql"
	"errors"
	"fmt"
	"jiacrontab/models"
	"jiacrontab/pkg/proto"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/kataras/iris"
)

type CustomerClaims struct {
	jwt.StandardClaims
	UserID   uint
	Mail     string
	Username string
	GroupID  uint
	Root     bool
}

// Login 用户登录
func Login(c iris.Context) {
	var (
		err            error
		ctx            = wrapCtx(c)
		reqBody        LoginReqParams
		user           models.User
		customerClaims CustomerClaims
	)

	if err = reqBody.verify(ctx); err != nil {
		ctx.respBasicError(err)
		return
	}
	if !user.Verify(reqBody.Username, reqBody.Passwd) {
		ctx.respAuthFailed(errors.New("帐号或密码不正确"))
		return
	}

	customerClaims.ExpiresAt = cfg.Jwt.Expires + time.Now().Unix()
	customerClaims.Username = reqBody.Username
	customerClaims.UserID = user.ID
	customerClaims.Mail = user.Mail
	customerClaims.GroupID = user.GroupID
	customerClaims.Root = user.Root

	if reqBody.Remember {
		customerClaims.ExpiresAt = time.Now().Add(24 * 30 * time.Hour).Unix()
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, customerClaims).SignedString([]byte(cfg.Jwt.SigningKey))

	if err != nil {
		ctx.respAuthFailed(errors.New("无法生成访问凭证"))
		return
	}

	ctx.respSucc("", map[string]interface{}{
		"token":   token,
		"groupID": user.GroupID,
		"root":    user.Root,
		"mail":    user.Mail,
		"userID":  user.ID,
	})
}

func getActivityList(c iris.Context) {
	var (
		ctx     = wrapCtx(c)
		err     error
		reqBody ReadMoreReqParams
		events  []models.Event
	)

	if err = reqBody.verify(ctx); err != nil {
		ctx.respError(proto.Code_Error, err.Error(), nil)
		return
	}

	if err = ctx.parseClaimsFromToken(); err != nil {
		ctx.respBasicError(err)
		return
	}

	err = models.DB().Where("user_id=? and id>?", ctx.claims.UserID, reqBody.LastID).
		Order(fmt.Sprintf("create_at %s", reqBody.Orderby)).
		Limit(reqBody.Pagesize).
		Find(&events).Error

	if err != nil && err != sql.ErrNoRows {
		ctx.respDBError(err)
		return
	}

	ctx.respSucc("", map[string]interface{}{
		"list":     events,
		"pagesize": reqBody.Pagesize,
	})
}

func getJobHistory(c iris.Context) {
	var (
		ctx      = wrapCtx(c)
		err      error
		reqBody  ReadMoreReqParams
		historys []models.JobHistory
		addrs    []string
	)

	if err = reqBody.verify(ctx); err != nil {
		ctx.respError(proto.Code_Error, err.Error(), nil)
		return
	}

	if addrs, err = ctx.getGroupAddr(); err != nil {
		ctx.respError(proto.Code_Error, err.Error(), err)
		return
	}

	err = models.DB().Where("addr in (?)", addrs).Order(fmt.Sprintf("create_at %s", reqBody.Orderby)).
		Find(&historys).Error

	if err != nil {
		ctx.respError(proto.Code_Error, "暂无数据", err)
		return
	}

	ctx.respSucc("", map[string]interface{}{
		"list":     historys,
		"pagesize": reqBody.Pagesize,
	})
}

func AuditJob(c iris.Context) {
	var (
		ctx     = wrapCtx(c)
		err     error
		reqBody AuditJobReqParams
		reply   bool
	)

	if err = reqBody.verify(ctx); err != nil {
		ctx.respBasicError(err)
		return
	}

	if !ctx.verifyNodePermission(reqBody.Addr) {
		ctx.respNotAllowed()
		return
	}

	if ctx.claims.GroupID != 0 {
		if ctx.claims.Root == false {
			ctx.respNotAllowed()
			return
		}
	}

	if reqBody.JobType == "crontab" {
		if err = rpcCall(reqBody.Addr, "CrontabJob.Audit", proto.AuditJobArgs{
			JobIDs: reqBody.JobIDs,
		}, &reply); err != nil {
			ctx.respRPCError(err)
			return
		}
		ctx.pubEvent(event_AuditCrontabJob, reqBody.Addr, reqBody)
	} else {
		if err = rpcCall(reqBody.Addr, "DaemonJob.Audit", proto.AuditJobArgs{
			JobIDs: reqBody.JobIDs,
		}, &reply); err != nil {
			ctx.respRPCError(err)
			return
		}
		ctx.pubEvent(event_AuditDaemonJob, reqBody.Addr, reqBody)
	}

	ctx.respSucc("", reply)

}

// IninAdminUser 初始化管理员
func IninAdminUser(c iris.Context) {
	var (
		err     error
		ctx     = wrapCtx(c)
		user    models.User
		reqBody UserReqParams
	)

	if err = reqBody.verify(ctx); err != nil {
		ctx.respBasicError(err)
		return
	}

	if !cfg.App.FirstUse || user.GroupID != 0 {
		ctx.respNotAllowed()
		return
	}

	user.Username = reqBody.Username
	user.Passwd = reqBody.Passwd
	user.Root = true
	user.Mail = reqBody.Mail

	if err = user.Create(); err != nil {
		ctx.respError(proto.Code_Error, err.Error(), nil)
		return
	}

	cfg.SetUsed()
	ctx.pubEvent(event_SignUpUser, "", reqBody)
	ctx.respSucc("", true)
}

// Signup 注册新用户
func Signup(c iris.Context) {
	var (
		err     error
		ctx     = wrapCtx(c)
		user    models.User
		reqBody UserReqParams
	)

	if err = reqBody.verify(ctx); err != nil {
		ctx.respBasicError(err)
		return
	}

	if reqBody.GroupID == 0 {
		ctx.respNotAllowed()
		return
	}

	user.Username = reqBody.Username
	user.Passwd = reqBody.Passwd
	user.GroupID = reqBody.GroupID
	user.Root = reqBody.Root
	user.Mail = reqBody.Mail

	if err = user.Create(); err != nil {
		ctx.respError(proto.Code_Error, err.Error(), nil)
		return
	}

	ctx.pubEvent(event_SignUpUser, "", reqBody)
	ctx.respSucc("", true)
}