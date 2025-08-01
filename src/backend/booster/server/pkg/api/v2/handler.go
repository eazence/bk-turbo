/*
 * Copyright (c) 2021 THL A29 Limited, a Tencent company. All rights reserved
 *
 * This source code file is licensed under the MIT License, you may obtain a copy of the License at
 *
 * http://opensource.org/licenses/MIT
 *
 */

package v2

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/blog"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/codec"
	commonTypes "github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/types"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/server/pkg/api"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/server/pkg/engine"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/server/pkg/manager"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/server/pkg/types"

	"github.com/emicklei/go-restful"
)

// Health return ok to caller
func Health(_ *restful.Request, resp *restful.Response) {
	api.ReturnRest(&api.RestResponse{Resp: resp})
}

// ApplyResource handle the http request for applying a new task.
func ApplyResource(req *restful.Request, resp *restful.Response) {
	param, err := getApplyParam(req)
	if err != nil {
		blog.Errorf("apply resource: get param failed, url(%s): %v", req.Request.URL.String(), err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrInvalidParam, Message: err.Error()})
		return
	}

	tb, err := defaultManager.CreateTask(param)
	if err != nil {
		if err == engine.ErrorProjectNoFound || strings.Contains(err.Error(), types.ErrorConcurrencyLimit.Error()) {
			blog.Warnf("apply resource: create task failed, url(%s): %v", req.Request.URL.String(), err)
		} else {
			blog.Errorf("apply resource: create task failed, url(%s): %v", req.Request.URL.String(), err)
		}
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrApplyResourceFailed, Message: err.Error()})
		return
	}

	info, err := getTaskInfo(tb.ID)
	if err != nil {
		blog.Errorf("apply resource: get task info(%s) from engine(%s) failed, url(%s): %v",
			tb.ID, tb.Client.EngineName.String(), req.Request.URL.String(), err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrRequestTaskInfoFailed, Message: err.Error()})
		return
	}

	blog.Infof("success to apply resource for task(%s) of project(%s) in engine(%s)",
		tb.ID, tb.Client.ProjectID, tb.Client.EngineName.String())
	api.ReturnRest(&api.RestResponse{Resp: resp, Data: info})
}

// SendMessage handle the http request for sending message, task message or project message.
// task message:    taskID should be provided, the message can be a request to collect the task stats data.
// project message: projectID should be provided, the message can be a project settings query.
func SendMessage(req *restful.Request, resp *restful.Response) {
	param, err := getMessageParam(req)
	if err != nil {
		blog.Errorf("send message: get param failed, url(%s): %v", req.Request.URL.String(), err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrInvalidParam, Message: err.Error()})
		return
	}

	var data []byte
	switch param.Type {
	case MessageTask:
		if data, err = defaultManager.SendTaskMessage(param.TaskID, []byte(param.Extra)); err != nil {
			blog.Errorf("send message: send task(%s) message to engine failed, url(%s) message(%s): %v",
				param.TaskID, req.Request.URL.String(), param.Extra, err)
			api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrSendMessageFailed, Message: err.Error()})
			return
		}
	case MessageProject:
		if param.ProjectID == "" {
			ip, _, err := net.SplitHostPort(req.Request.RemoteAddr)
			if err == nil && net.ParseIP(ip) != nil {
				blog.Infof("send message: request from client(%s) has null project id", ip)
			}
			blog.Infof("send message: got null project id, not sent")
			api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrSendMessageFailed, Message: "null project id"})
			return
		}
		if data, err = defaultManager.SendProjectMessage(param.ProjectID, []byte(param.Extra)); err != nil {
			blog.Warnf("send message: send project(%s) message to engine failed, url(%s) message(%s): %v",
				param.ProjectID, req.Request.URL.String(), param.Extra, err)
			api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrSendMessageFailed, Message: err.Error()})
			return
		}
	default:
		blog.Errorf("send message: get unknown message type(%s) from param, url(%s) message(%s): %v",
			param.Type, req.Request.URL.String(), param.Extra, err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrUnknownMessageType, Message: err.Error()})
		return
	}

	api.ReturnRest(&api.RestResponse{Resp: resp, Data: data})
}

// QueryTaskInfo handle the http request for querying a existing task
func QueryTaskInfo(req *restful.Request, resp *restful.Response) {
	taskID := req.QueryParameter(queryTaskIDKey)
	if taskID == "" {
		blog.Errorf("query task info: query failed, url(%s): taskID not specified", req.Request.URL.String())
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrInvalidParam,
			Message: "task_id no specific"})
		return
	}

	info, err := getTaskInfo(taskID)
	if err != nil {
		blog.Errorf("apply resource: get task info(%s) failed, url(%s): %v",
			taskID, req.Request.URL.String(), err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrRequestTaskInfoFailed, Message: err.Error()})
		return
	}

	api.ReturnRest(&api.RestResponse{Resp: resp, Data: info})
}

// UpdateHeartbeat handle the http request for updating task heartbeat.
func UpdateHeartbeat(req *restful.Request, resp *restful.Response) {
	taskID, err := getHeartbeatParam(req)
	if err != nil {
		blog.Errorf("update heartbeat: getHeartbeat failed, url(%s): %v", req.Request.URL.String(), err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrInvalidParam, Message: err.Error()})
		return
	}

	blog.Infof("update heartbeat: try to update task(%s) heartbeat", taskID)
	status, err := defaultManager.UpdateHeartbeat(taskID)
	if err != nil {
		blog.Warnf("update heartbeat: update task(%s) heartbeat failed, url(%s): %v",
			taskID, req.Request.URL.String(), err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrUpdateHeartbeatFailed,
			Message: api.ServerErrUpdateHeartbeatFailed.String() + ":" + err.Error()})
		return
	}

	blog.Infof("update heartbeat: success to update task(%s) heartbeat", taskID)
	api.ReturnRest(&api.RestResponse{Resp: resp, Data: &RespHeartbeat{
		TaskID: taskID,
		Type:   HeartBeatPong.String(),
		Status: status,
	}})
}

// ReleaseResource handle the http request for releasing a existing task
// it return immediately after the task saved into database with status finish or failed.
func ReleaseResource(req *restful.Request, resp *restful.Response) {
	param, err := getReleaseParam(req)
	if err != nil {
		blog.Errorf("release resource: get param failed, url(%s): %v", req.Request.URL.String(), err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrInvalidParam, Message: err.Error()})
		return
	}

	if err = defaultManager.ReleaseTask(param); err != nil {
		blog.Errorf("release resource: release task(%s) failed, url(%s): %v",
			param.TaskID, req.Request.URL.String(), err)

		if err == engine.ErrorUnterminatedTaskNoFound || err == types.ErrorTaskAlreadyTerminated {
			api.ReturnRest(&api.RestResponse{Resp: resp, Message: err.Error()})
			return
		}
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrReleaseResourceFailed, Message: err.Error()})
		return
	}

	blog.Infof("success to release resource for task(%s)", param.TaskID)
	api.ReturnRest(&api.RestResponse{Resp: resp})
}

func getApplyParam(req *restful.Request) (*manager.TaskCreateParam, error) {
	body, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		blog.Errorf("get apply param: get request body failed: %v", err)
		return nil, err
	}

	blog.Infof("get apply param: %s", string(body))

	var protocol ParamApply
	if err = codec.DecJSON(body, &protocol); err != nil {
		blog.Errorf("get apply param: decode failed: %v, body: %s", err, string(body))
		return nil, err
	}

	// get client IP
	var clientIP string
	if strings.Count(req.Request.Header.Get(api.HeaderRemote), ":") >= 2 { //ipv6
		clientIP = req.Request.Header.Get(api.HeaderRemote)
	} else { //ipv4
		arr := strings.Split(req.Request.Header.Get(api.HeaderRemote), ":")
		if len(arr) > 0 {
			remoteIP := arr[0]
			blog.Infof("get apply param: get remote ip [%s]", remoteIP)

			// use remoteIP for clientIP check, never trust the client one.
			clientIP = remoteIP
		}
	}

	param := &manager.TaskCreateParam{
		ProjectID:     commonTypes.GetProjectIDWithScene(protocol.ProjectID, protocol.Scene),
		BuildID:       protocol.BuildID,
		ClientVersion: protocol.ClientVersion,
		ClientCPU:     protocol.ClientCPU,
		Message:       protocol.Message,
		Extra:         protocol.Extra,
		ClientIP:      clientIP,
	}

	blog.Infof("get apply param: get client status(project ID/client IP/user version: %s %s %s",
		param.ProjectID, param.ClientIP, param.ClientVersion)
	return param, nil
}

func getMessageParam(req *restful.Request) (*ParamMessage, error) {
	body, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		blog.Errorf("get message param: get request body failed: %v", err)
		return nil, err
	}

	var protocol ParamMessage
	if err = codec.DecJSON(body, &protocol); err != nil {
		blog.Errorf("get message param: decode failed: %v, body: %s", err, string(body))
		return nil, err
	}

	if protocol.Type == "" {
		protocol.Type = MessageTask
	}
	protocol.ProjectID = commonTypes.GetProjectIDWithScene(protocol.ProjectID, protocol.Scene)

	blog.Debugf("get message param: get message: %s", string(body))
	return &protocol, nil
}

func getTaskInfo(taskID string) (*RespTaskInfo, error) {
	tb, te, err := defaultManager.GetTask(taskID)
	if err != nil {
		blog.Errorf("get apply param: get task(%s) failed: %v", taskID, err)
		return nil, err
	}

	rank := 0
	if tb.Status.Status == engine.TaskStatusStaging {
		rank, err = defaultManager.GetTaskRank(taskID)
		if err != nil {
			blog.Warnf("get apply param: get task(%s) rank from engine(%s) queue(%s) failed: %v",
				taskID, tb.Client.EngineName.String(), tb.Client.QueueName, err)
			rank = 0
		}
	}

	hostlist := []string{}
	hostNameMap := map[string]string{}
	extra := ""
	if te != nil {
		hostlist = te.WorkerList()
		hostNameMap = te.GetWorkerNameMap()
		extra = string(te.Dump())
	}

	return &RespTaskInfo{
		TaskID:      tb.ID,
		Status:      tb.Status.Status,
		HostList:    hostlist,
		HostNameMap: hostNameMap,
		QueueNumber: rank,
		Message:     tb.Status.Message,
		Extra:       extra,
	}, nil
}

func getHeartbeatParam(req *restful.Request) (string, error) {
	body, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		blog.Errorf("get heartbeat param: get request body failed: %v", err)
		return "", err
	}

	var protocol ParamHeartbeat
	if err = codec.DecJSON(body, &protocol); err != nil {
		blog.Errorf("get heartbeat param: decode failed: %v, body: %s", err, string(body))
		return "", err
	}

	blog.Debugf("get heartbeat param: get heartbeat: %s", string(body))
	return protocol.TaskID, nil
}

func getReleaseParam(req *restful.Request) (*manager.TaskReleaseParam, error) {
	body, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		blog.Errorf("get release param: get request body failed: %v", err)
		return nil, err
	}

	var protocol ParamRelease
	if err = codec.DecJSON(body, &protocol); err != nil {
		blog.Errorf("get release param: decode failed: %v, body: %s", err, string(body))
		return nil, err
	}

	param := &manager.TaskReleaseParam{
		TaskID:  protocol.TaskID,
		Message: protocol.Message,
		Success: protocol.Success,
		Extra:   protocol.Extra,
	}

	blog.Infof("get release param: %s", string(body))
	return param, nil
}

// QueryWorkerUpgradeInfo handle the http request for querying worker upgrade info
func QueryWorkerUpgradeInfo(req *restful.Request, resp *restful.Response) {
	conf := "./conf/worker_upgrade.json"
	data, err := ioutil.ReadFile(conf)
	if err != nil {
		blog.Warnf("get conf: %s failed with error:%v", conf, err)
		data = []byte("")
	}
	api.ReturnRest(&api.RestResponse{Resp: resp, Data: data})
}

// QueryCacheList handle the http request for querying cache list
func QueryCacheList(req *restful.Request, resp *restful.Response) {
	projectID := req.QueryParameter(queryProjectIDKey)
	if projectID == "" {
		blog.Errorf("query cache list: url(%s): projectID not specified", req.Request.URL.String())
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrInvalidParam,
			Message: "project_id no specific"})
		return
	}

	// 打开JSON文件
	file, err := os.Open(cacheListFile)
	if err != nil {
		blog.Errorf("query cache list: url(%s) open %s with error:%v",
			req.Request.URL.String(), cacheListFile, err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrReadJSONFailed,
			Message: "not found cache list"})
		return
	}
	defer file.Close()

	// 读取文件内容
	bytes, err := io.ReadAll(file)
	if err != nil {
		blog.Errorf("query cache list: url(%s) read %s with error:%v",
			req.Request.URL.String(), cacheListFile, err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrReadJSONFailed,
			Message: "not found cache list"})
		return
	}

	// 解析JSON到结构体
	var config CacheConfigList
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		blog.Errorf("query cache list: url(%s) Unmarshal %s with error:%v",
			req.Request.URL.String(), cacheListFile, err)
		api.ReturnRest(&api.RestResponse{Resp: resp, ErrCode: api.ServerErrReadJSONFailed,
			Message: "not found cache list"})
		return
	}

	// 过滤
	configfilter := make(map[string]CacheConfig)
	v, ok := config[projectID]
	if ok {
		configfilter[projectID] = v
	} else {
		v, ok = config[cacheDefaultProjectID]
		if ok {
			configfilter[cacheDefaultProjectID] = v
		}
	}

	api.ReturnRest(&api.RestResponse{Resp: resp, Data: configfilter})
}
