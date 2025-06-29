/*
 * Copyright (c) 2021 THL A29 Limited, a Tencent company. All rights reserved
 *
 * This source code file is licensed under the MIT License, you may obtain a copy of the License at
 *
 * http://opensource.org/licenses/MIT
 *
 */

package pkg

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/booster/pkg"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/env"
	dcFile "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/file"
	dcSDK "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/sdk"
	dcType "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/types"
	dcUtil "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/util"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/controller/pkg/api"
	v1 "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/controller/pkg/api/v1"

	// "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/ubttool/command"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/ubttool/common"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/blog"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/codec"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/util"

	shaderToolComm "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/shadertool/common"

	"github.com/google/shlex"
)

const (
	OSWindows         = "windows"
	MaxWaitSecs       = 10800
	TickSecs          = 30
	DefaultJobs       = 240 // ok for most machines
	ActionDescMaxSize = 50

	DevOPSProcessTreeKillKey = "DEVOPS_DONT_KILL_PROCESS_TREE"

	UbaAgentTemplate      = "bk_uba_action_template_ubt.json"
	UbaTemplateToolKey    = "${tool_key}"
	UbaTemplateToolKeyDir = "${tool_key_dir}"
	UbaTemplateHostIP     = "${host_ip}"
)

// NewUBTTool get a new UBTTool
// func NewUBTTool(flagsparam *common.Flags, config dcSDK.ControllerConfig) *UBTTool {
func NewUBTTool(flagsparam *common.Flags) *UBTTool {
	// blog.Infof("UBTTool: new ubt tool with config:%+v,flags:%+v", config, *flagsparam)
	blog.Infof("UBTTool: new ubt tool with flags:%+v", *flagsparam)

	return &UBTTool{
		flags: flagsparam,
		// controller:     v1.NewSDK(config),
		allactions:     []common.Action{},
		readyactions:   []common.Action{},
		finishednumber: 0,
		runningnumber:  0,
		maxjobs:        0,
		finished:       false,
		actionchan:     nil,
		// executor:       NewExecutor(),
		moduleselected: make(map[string]int, 0),
	}
}

// UBTTool describe the ubt tool handler
type UBTTool struct {
	flags *common.Flags

	controller dcSDK.ControllerSDK

	// lock for full actions
	allactionlock sync.RWMutex
	allactions    []common.Action

	// lock for ready actions
	readyactionlock sync.RWMutex
	readyactions    []common.Action

	finishednumberlock sync.Mutex
	finishednumber     int32
	runningnumber      int32
	maxjobs            int32
	finished           bool

	moduleselected map[string]int

	actionchan chan common.Actionresult

	booster  *pkg.Booster
	executor *Executor

	// settings
	projectSettingFile string
	settings           *shaderToolComm.ApplyParameters

	// whether use web socket
	usewebsocket bool
}

// Run run the tool
func (h *UBTTool) Run(ctx context.Context) (int, error) {
	return h.run(ctx)
}

func (h *UBTTool) run(pCtx context.Context) (int, error) {
	// update path env
	err := h.initsettings()
	if err != nil {
		return GetErrorCode(err), err
	}

	blog.Infof("UBTTool: try to find controller or launch it")
	// support dinamic listen port
	var port int
	_, port, err = h.controller.EnsureServer()
	if err != nil {
		blog.Errorf("UBTTool: ensure controller failed: %v", err)
		return 1, err
	}

	blog.Infof("UBTTool: success to connect to controller with port[%d]", port)
	os.Setenv(env.GetEnvKey(env.KeyExecutorControllerPort), strconv.Itoa(port))
	blog.Infof("UBTTool: set env %s=%d]", env.GetEnvKey(env.KeyExecutorControllerPort), port)

	// executor依赖动态端口
	h.executor = NewExecutor()

	if !h.executor.Valid() {
		blog.Errorf("UBTTool: ensure controller failed: %v", ErrorInvalidWorkID)
		return 1, ErrorInvalidWorkID
	}
	h.executor.usewebsocket = h.usewebsocket

	// run actions now
	err = h.runActions()
	if err != nil {
		return 1, err
	}

	return 0, nil
}

// 过滤ip，客户端判断不保险，最好是能够从server拿到ip
func isIPValid(ip string) bool {
	if strings.HasPrefix(ip, "192.") ||
		strings.HasPrefix(ip, "172.") ||
		strings.HasPrefix(ip, "127.") {
		return false
	}

	return true
}

func (h *UBTTool) adjustActions4UBAAgent(all *common.UE4Action) {
	if strings.HasSuffix(h.flags.ActionChainFile, UbaAgentTemplate) {
		blog.Debugf("UBTTool: ready adjust for %s", UbaAgentTemplate)

		data, err := os.ReadFile(h.flags.ToolChainFile)
		if err != nil {
			blog.Errorf("failed to read tool chain json file %s with error %v", h.flags.ToolChainFile, err)
			return
		}

		var t dcSDK.Toolchain
		if err = codec.DecJSON(data, &t); err != nil {
			blog.Errorf("failed to decode json content[%s] failed: %v", string(data), err)
			return
		}

		if len(t.Toolchains) == 0 {
			blog.Errorf("tool chain is empty")
			return
		}

		for i := range all.Actions {
			if all.Actions[i].Cmd == UbaTemplateToolKey {
				all.Actions[i].Cmd = t.Toolchains[0].ToolKey
			}

			if all.Actions[i].Workdir == UbaTemplateToolKeyDir {
				all.Actions[i].Workdir = filepath.Dir(t.Toolchains[0].ToolKey)
			}

			ips := util.GetIPAddress()
			if len(ips) > 0 {
				ip := ips[0]
				for _, ipstr := range ips {
					if isIPValid(ipstr) {
						ip = ipstr
						break
					}
				}
				if strings.ContainsAny(all.Actions[i].Arg, UbaTemplateHostIP) {
					all.Actions[i].Arg = strings.Replace(all.Actions[i].Arg, UbaTemplateHostIP, ip, -1)
				}
			}
		}

		blog.Infof("after adjust,actions: %v", *all)
	}
}

func (h *UBTTool) runActions() error {
	blog.Infof("UBTTool: try to run actions")

	if h.flags.ActionChainFile == "" || h.flags.ActionChainFile == "nothing" {
		blog.Debugf("UBTTool: action json file not set, do nothing now")
		return nil
	}

	all, err := resolveActionJSON(h.flags.ActionChainFile)
	if err != nil {
		blog.Warnf("UBTTool: failed to resolve %s with error:%v", h.flags.ActionChainFile, err)
		return err
	}
	// for debug
	blog.Debugf("UBTTool: all actions:%+v", all)

	// support uba agent template
	h.adjustActions4UBAAgent(all)

	// execute actions here
	h.allactions = all.Actions

	// parse actions firstly
	h.analyzeActions(h.allactions)
	// for debug
	blog.Debugf("UBTTool: all actions:%+v", h.allactions)

	// readyactions includes actions which no depend
	err = h.getReadyActions()
	if err != nil {
		blog.Warnf("UBTTool: failed to get ready actions with error:%v", err)
		return err
	}

	err = h.executeActions()
	if err != nil {
		blog.Warnf("UBTTool: failed to run actions with error:%v", err)
		return err
	}

	blog.Debugf("UBTTool: success to execute all %d actions", len(h.allactions))
	return nil
}

// execute actions got from ready queue
func (h *UBTTool) executeActions() error {
	h.maxjobs = DefaultJobs

	// get max jobs from env
	maxjobstr := env.GetEnv(env.KeyCommonUE4MaxJobs)
	maxjobs, err := strconv.Atoi(maxjobstr)
	if err != nil {
		blog.Infof("UBTTool: failed to get jobs by UE4_MAX_PROCESS with error:%v", err)
	} else {
		h.maxjobs = int32(maxjobs)
	}

	fmt.Fprintf(os.Stderr, "UBTTool: Building %d actions with %d jobs...", len(h.allactions), h.maxjobs)

	// h.dump()

	// execute actions no more than max jobs
	blog.Infof("UBTTool: try to run actions up to %d jobs", h.maxjobs)
	h.actionchan = make(chan common.Actionresult, h.maxjobs)

	// execute first batch actions
	h.selectActionsToExecute()
	if h.runningnumber <= 0 {
		blog.Errorf("UBTTool: faile to execute actions with error:%v", ErrorNoActionsToRun)
		return ErrorNoActionsToRun
	}

	for {
		tick := time.NewTicker(TickSecs * time.Second)
		starttime := time.Now()
		select {
		case r := <-h.actionchan:
			blog.Infof("UBTTool: got action result:%+v", r)
			if (r.Exitcode != 0 || r.Err != nil) && !h.settings.ContinueOnError {
				err := fmt.Errorf("exit code:%d,error:%v", r.Exitcode, r.Err)
				blog.Errorf("UBTTool: %v", err)
				return err
			}
			h.onActionFinished(r.Index, r.Exitcode)
			if h.finished {
				blog.Infof("UBTTool: all actions finished")
				return nil
			}
			h.selectActionsToExecute()
			if h.runningnumber <= 0 {
				blog.Errorf("UBTTool: faile to execute actions with error:%v", ErrorNoActionsToRun)
				return ErrorNoActionsToRun
			}

		case <-tick.C:
			curtime := time.Now()
			blog.Infof("start time [%s] current time [%s] ", starttime, curtime)
			if curtime.Sub(starttime) > (time.Duration(MaxWaitSecs) * time.Second) {
				blog.Errorf("UBTTool: faile to execute actions with error:%v", ErrorOverMaxTime)
				return ErrorOverMaxTime
			}
			// h.dump()
		}
	}
}

// // to simply print log
// func getActionDesc(cmd, arg string) string {
// 	// _, _ = fmt.Fprintf(os.Stdout, "cmd %s arg %s\n", cmd, arg)

// 	exe := filepath.Base(cmd)
// 	targetsuffix := []string{}
// 	switch exe {
// 	case "cl.exe", "cl-filter.exe", "clang.exe", "clang++.exe", "clang", "clang++":
// 		targetsuffix = []string{".cpp", ".c", ".response\"", ".response"}
// 		break
// 	case "lib.exe", "link.exe", "link-filter.exe":
// 		targetsuffix = []string{".dll", ".lib", ".response\"", ".response"}
// 	default:
// 		return exe
// 	}

// 	args, _ := shlex.Split(replaceWithNextExclude(arg, '\\', "\\\\", []byte{'"'}))
// 	if len(args) == 1 {
// 		argbase := strings.TrimRight(filepath.Base(arg), "\"")
// 		return fmt.Sprintf("%s %s", exe, argbase)
// 	} else {
// 		for _, v := range args {
// 			for _, s := range targetsuffix {
// 				if strings.HasSuffix(v, s) {
// 					vtrime := strings.TrimRight(v, "\"")
// 					return fmt.Sprintf("%s %s", exe, vtrime)
// 				}
// 			}
// 		}
// 	}

// 	return exe
// }

// to simply print log
func (h *UBTTool) analyzeActions(actions []common.Action) error {
	for i, v := range actions {
		cmd := v.Cmd
		arg := v.Arg
		exe := filepath.Base(cmd)
		targetsuffix := []string{}
		needAnalyzeArg := true
		switch exe {
		case "cl.exe", "cl-filter.exe", "clang.exe",
			"clang++.exe", "clang", "clang++",
			"prospero-clang.exe", "clang-cl.exe":
			targetsuffix = []string{".cpp", ".c", ".response\"", ".response"}
			actions[i].IsCompile = true
			break
		case "lib.exe", "link.exe", "link-filter.exe":
			targetsuffix = []string{".dll", ".lib", ".response\"", ".response"}
		default:
			needAnalyzeArg = false
			arglen := ActionDescMaxSize
			if arglen > len(arg) {
				arglen = len(arg)
			}
			actions[i].Desc = fmt.Sprintf("%s %s...", exe, arg[0:arglen])
		}

		if !needAnalyzeArg {
			continue
		}

		args, _ := shlex.Split(replaceWithNextExclude(arg, '\\', "\\\\", []byte{'"'}))
		if len(args) == 1 {
			argbase := strings.TrimRight(filepath.Base(arg), "\"")
			actions[i].Desc = fmt.Sprintf("%s %s", exe, argbase)
			actions[i].ModulePath = filepath.Dir(arg)
		} else {
			foundSuffix := false
			for _, v := range args {
				foundSuffix = false
				for _, s := range targetsuffix {
					if strings.HasSuffix(v, s) {
						vtrime := strings.TrimRight(v, "\"")
						actions[i].Desc = fmt.Sprintf("%s %s", exe, vtrime)
						actions[i].ModulePath = filepath.Dir(v)
						foundSuffix = true
						break
					}
				}
				if foundSuffix {
					break
				}
			}

			if !foundSuffix {
				arglen := ActionDescMaxSize
				if arglen > len(arg) {
					arglen = len(arg)
				}
				actions[i].Desc = fmt.Sprintf("%s %s...", exe, arg[0:arglen])
			}
		}
	}

	totalcompilenum := 0
	for _, v := range actions {
		if v.IsCompile {
			totalcompilenum++
			if v.ModulePath != "" {
				if _, ok := h.moduleselected[v.ModulePath]; !ok {
					h.moduleselected[v.ModulePath] = 0
				}
			}
		}
	}

	env.SetEnv(env.KeyExecutorTotalActionNum, strconv.Itoa(totalcompilenum))
	blog.Infof("UBTTool: set total action num with: %s=%d", env.KeyExecutorTotalActionNum, totalcompilenum)

	return nil
}

func (h *UBTTool) selectActionsToExecute() error {
	h.readyactionlock.Lock()
	defer h.readyactionlock.Unlock()

	for h.runningnumber < h.maxjobs {
		index := h.selectReadyAction()
		if index < 0 { // no tasks to run
			return nil
		}

		h.readyactions[index].Running = true
		h.runningnumber++
		_, _ = fmt.Fprintf(os.Stdout, "[bk_ubt_tool] [%d/%d] %s\n",
			h.finishednumber+h.runningnumber, len(h.allactions), h.readyactions[index].Desc)
		// getActionDesc(h.readyactions[index].Cmd, h.readyactions[index].Arg))
		go h.executeOneAction(h.readyactions[index], h.actionchan)
	}

	return nil
}

func (h *UBTTool) selectReadyAction() int {
	index := -1
	followers := -1

	// select ready action which is not running and has most followers
	if h.flags.MostDepentFirst {
		for i := range h.readyactions {
			if !h.readyactions[i].Running {
				curfollowers := len(h.readyactions[i].FollowIndex)
				if curfollowers > followers {
					index = i
					followers = curfollowers
				}
			}
		}
	} else { // select first action which is not running
		for i := range h.readyactions {
			if !h.readyactions[i].Running {
				index = i
				break
			}
		}
	}

	if index >= 0 {
		blog.Infof("UBTTool: selected global index %s with %d followers", h.readyactions[index].Index, followers)
		if h.readyactions[index].IsCompile && h.readyactions[index].ModulePath != "" {
			h.moduleselected[h.readyactions[index].ModulePath]++
		}
	}
	return index
}

func (h *UBTTool) executeOneAction(action common.Action, actionchan chan common.Actionresult) error {
	blog.Infof("UBTTool: ready execute actions:%+v", action)

	blog.Infof("UBTTool: raw cmd:[%s %s]", action.Cmd, action.Arg)

	commandType := dcType.CommandDefault
	fullargs := []string{action.Cmd}
	if strings.HasSuffix(action.Cmd, "cmd.exe") || strings.HasSuffix(action.Cmd, "Cmd.exe") {
		fullargs = append(fullargs, action.Arg)
	} else if (strings.HasSuffix(action.Cmd, "ispc.exe") || strings.HasSuffix(action.Cmd, "Ispc.exe")) && len(action.Arg) > dcType.MaxWindowsCommandLength {
		fullargs = append(fullargs, action.Arg)
		commandType = dcType.CommandInFile
	} else {
		args, _ := shlex.Split(replaceWithNextExclude(action.Arg, '\\', "\\\\", []byte{'"'}))
		fullargs = append(fullargs, args...)
	}

	blog.Infof("UBTTool: sent cmd:[%s]", strings.Join(fullargs, " "))

	//exitcode, err := h.executor.Run(fullargs, action.Workdir)
	// try again if failed after sleep some time
	var retcode int
	retmsg := ""
	waitsecs := 5
	var err error
	for try := 0; try < 3; try++ {
		retcode, retmsg, err = h.executor.Run(fullargs, action.Workdir, commandType, action.Attributes)
		if retcode != int(api.ServerErrOK) {
			blog.Warnf("UBTTool: failed to execute action with ret code:%d error [%+v] for %d times, actions:%+v", retcode, err, try+1, action)

			if retcode == int(api.ServerErrWorkNotFound) {
				h.dealWorkNotFound(retcode, retmsg)
				continue
			} else {
				break
			}

			// time.Sleep(time.Duration(waitsecs) * time.Second)
			// waitsecs = waitsecs * 2
			// continue
		}

		if err != nil {
			blog.Warnf("UBTTool: failed to execute action with error [%+v] for %d times, actions:%+v", err, try+1, action)
			time.Sleep(time.Duration(waitsecs) * time.Second)
			waitsecs = waitsecs * 2
			continue
		}
		break
	}

	r := common.Actionresult{
		Index:     action.Index,
		Finished:  true,
		Succeed:   err == nil,
		Outputmsg: "",
		Errormsg:  "",
		Exitcode:  retcode,
		Err:       err,
	}

	actionchan <- r

	return nil
}

// get ready actions from all actions
func (h *UBTTool) getReadyActions() error {
	blog.Infof("UBTTool: try to get ready actions")

	h.allactionlock.Lock()
	defer h.allactionlock.Unlock()

	h.readyactionlock.Lock()
	defer h.readyactionlock.Unlock()

	// copy actions which no depend from all to ready
	for i, v := range h.allactions {
		if !v.Running && !v.Finished && len(v.Dep) == 0 {
			h.readyactions = append(h.readyactions, v)
			h.allactions[i].Running = true
		}
	}

	return nil
}

// update all actions and ready actions
func (h *UBTTool) onActionFinished(index string, exitcode int) error {
	blog.Infof("UBTTool: action %s finished with exitcode %d", index, exitcode)

	h.finishednumberlock.Lock()
	h.finishednumber++
	h.runningnumber--
	blog.Infof("UBTTool: running : %d, finished : %d, total : %d", h.runningnumber, h.finishednumber, len(h.allactions))
	if h.finishednumber >= int32(len(h.allactions)) {
		h.finishednumberlock.Unlock()
		blog.Infof("UBTTool: finishend,module selected:%+v", h.moduleselected)
		h.finished = true
		return nil
	}
	h.finishednumberlock.Unlock()

	// update with index
	h.allactionlock.Lock()
	defer h.allactionlock.Unlock()

	h.readyactionlock.Lock()
	defer h.readyactionlock.Unlock()

	// delete from ready array
	for i, v := range h.readyactions {
		if v.Index == index {
			h.readyactions = removeaction(h.readyactions, i)
			break
		}
	}

	// update status in allactions
	for i, v := range h.allactions {
		// update status
		if v.Index == index {
			h.allactions[i].Finished = true
			break
		}
	}

	// update depend in allactions if current action succeed
	if exitcode == 0 {
		for i, v := range h.allactions {
			if v.Finished {
				continue
			}

			// update depend
			for i1, v1 := range v.Dep {
				if v1 == index {
					h.allactions[i].Dep = remove(h.allactions[i].Dep, i1)
					break
				}
			}

			// copy to ready if no depent
			if !v.Running && !v.Finished && len(h.allactions[i].Dep) == 0 {
				h.readyactions = append(h.readyactions, v)
				h.allactions[i].Running = true
			}
		}
	}

	return nil
}

func (h *UBTTool) dump() {
	blog.Infof("UBTTool: +++++++++++++++++++dump start+++++++++++++++++++++")
	blog.Infof("UBTTool: finished:%d,running:%d,total:%d", h.finishednumber, h.runningnumber, len(h.allactions))

	for _, v := range h.allactions {
		blog.Infof("UBTTool: action:%+v", v)
	}
	blog.Infof("UBTTool: -------------------dump end-----------------------")
}

// ---------------------------------to support set tool chain----------------------------------------------------------
func (h *UBTTool) getControllerConfig() dcSDK.ControllerConfig {
	return dcSDK.ControllerConfig{
		NoLocal: false,
		Scheme:  common.ControllerScheme,
		IP:      common.ControllerIP,
		Port:    common.ControllerPort,
		Timeout: 5 * time.Second,
		LogDir:  h.flags.LogDir,
		LogVerbosity: func() int {
			// debug模式下, --v=3
			if h.flags.LogLevel == dcUtil.PrintDebug.String() {
				return 3
			}
			return 0
		}(),
		RemainTime:          h.settings.ControllerIdleRunSeconds,
		NoWait:              h.settings.ControllerNoBatchWait,
		SendCork:            h.settings.ControllerSendCork,
		SendFileMemoryLimit: h.settings.ControllerSendFileMemoryLimit,
		NetErrorLimit:       h.settings.ControllerNetErrorLimit,
		RemoteRetryTimes:    h.settings.ControllerRemoteRetryTimes,
		EnableLink:          h.settings.ControllerEnableLink,
		EnableLib:           h.settings.ControllerEnableLib,
		LongTCP:             h.settings.ControllerLongTCP,
		DynamicPort:         h.settings.ControllerDynamicPort,
		PreferLocal:         h.settings.ControllerPreferLocal,
	}
}

func (h *UBTTool) initsettings() error {
	var err error
	h.projectSettingFile, err = h.getProjectSettingFile()
	if err != nil {
		return err
	}

	h.settings, err = resolveApplyJSON(h.projectSettingFile)
	if err != nil {
		return err
	}

	blog.Infof("UBTTool: loaded setting:%+v", *h.settings)

	for k, v := range h.settings.Env {
		blog.Infof("UBTTool: set env %s=%s", k, v)
		os.Setenv(k, v)

		if k == "BK_DIST_LOG_LEVEL" {
			common.SetLogLevel(v)
		} else if k == "BK_DIST_USE_WEBSOCKET" {
			h.usewebsocket = true
		}
	}
	os.Setenv(DevOPSProcessTreeKillKey, "true")

	h.controller = v1.NewSDK(h.getControllerConfig())

	return nil
}

func (h *UBTTool) getProjectSettingFile() (string, error) {
	exepath := dcUtil.GetExcPath()
	if exepath != "" {
		jsonfile := filepath.Join(exepath, "bk_project_setting.json")
		if dcFile.Stat(jsonfile).Exist() {
			return jsonfile, nil
		}
	}

	return "", ErrorProjectSettingNotExisted
}

func resolveApplyJSON(filename string) (*shaderToolComm.ApplyParameters, error) {
	blog.Infof("resolve apply json file %s", filename)

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		blog.Errorf("failed to read apply json file %s with error %v", filename, err)
		return nil, err
	}

	var t shaderToolComm.ApplyParameters
	if err = codec.DecJSON(data, &t); err != nil {
		blog.Errorf("failed to decode json content[%s] failed: %v", string(data), err)
		return nil, err
	}

	return &t, nil
}

func (h *UBTTool) newBooster() (*pkg.Booster, error) {
	blog.Infof("UBTTool: new booster in...")

	// get current running dir.
	// if failed, it doesn't matter, just give a warning.
	runDir, err := os.Getwd()
	if err != nil {
		blog.Warnf("booster-command: get working dir failed: %v", err)
	}

	// get current user, use the login username.
	// if failed, it doesn't matter, just give a warning.
	usr, err := user.Current()
	if err != nil {
		blog.Warnf("booster-command: get current user failed: %v", err)
	}

	// decide which server to connect to.
	ServerHost := h.settings.ServerHost
	projectID := h.settings.ProjectID
	buildID := h.settings.BuildID

	// generate a new booster.
	cmdConfig := dcType.BoosterConfig{
		Type:      dcType.BoosterType(h.settings.Scene),
		ProjectID: projectID,
		BuildID:   buildID,
		BatchMode: h.settings.BatchMode,
		Args:      "",
		Cmd:       strings.Join(os.Args, " "),
		Works: dcType.BoosterWorks{
			Stdout:            os.Stdout,
			Stderr:            os.Stderr,
			RunDir:            runDir,
			User:              usr.Username,
			WorkerList:        h.settings.WorkerList,
			LimitPerWorker:    h.settings.LimitPerWorker,
			MaxLocalTotalJobs: defaultCPULimit(h.settings.MaxLocalTotalJobs),
			MaxLocalPreJobs:   h.settings.MaxLocalPreJobs,
			MaxLocalExeJobs:   h.settings.MaxLocalExeJobs,
			MaxLocalPostJobs:  h.settings.MaxLocalPostJobs,
			ResultCacheList:   h.settings.ResultCacheList,
		},

		Transport: dcType.BoosterTransport{
			ServerHost:             ServerHost,
			Timeout:                5 * time.Second,
			HeartBeatTick:          5 * time.Second,
			InspectTaskTick:        1 * time.Second,
			TaskPreparingTimeout:   60 * time.Second,
			PrintTaskInfoEveryTime: 5,
			CommitSuicideCheckTick: 5 * time.Second,
		},

		// got controller listen port from local file
		Controller: dcSDK.ControllerConfig{
			NoLocal: false,
			Scheme:  shaderToolComm.ControllerScheme,
			IP:      shaderToolComm.ControllerIP,
			Port:    shaderToolComm.ControllerPort,
			Timeout: 5 * time.Second,
		},
	}

	return pkg.NewBooster(cmdConfig)
}

func (h *UBTTool) setToolChain() error {
	blog.Infof("UBTTool: set toolchain in...")

	var err error
	if h.booster == nil {
		h.booster, err = h.newBooster()
		if err != nil || h.booster == nil {
			blog.Errorf("UBTTool: failed to new booster: %v", err)
			return err
		}
	}

	err = h.booster.SetToolChain(h.flags.ToolChainFile)
	if err != nil {
		blog.Errorf("UBTTool: failed to set tool chain, error: %v", err)
	}

	return err
}

func (h *UBTTool) dealWorkNotFound(retcode int, retmsg string) error {
	blog.Infof("UBTTool: deal for work not found with code:%d, msg:%s", retcode, retmsg)

	if retmsg == "" {
		return nil
	}

	msgs := strings.Split(retmsg, "|")
	if len(msgs) < 2 {
		return nil
	}

	var workerid v1.WorkerChanged
	if err := codec.DecJSON([]byte(msgs[1]), &workerid); err != nil {
		blog.Errorf("UBTTool: decode param %s failed: %v", msgs[1], err)
		return err
	}

	if workerid.NewWorkID != "" {
		// update local workid with new workid
		env.SetEnv(env.KeyExecutorControllerWorkID, workerid.NewWorkID)
		h.executor.Update()

		// set tool chain with new workid
		h.setToolChain()
	}

	return nil
}
