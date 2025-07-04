//go:build darwin
// +build darwin

/*
 * Copyright (c) 2021 THL A29 Limited, a Tencent company. All rights reserved
 *
 * This source code file is licensed under the MIT License, you may obtain a copy of the License at
 *
 * http://opensource.org/licenses/MIT
 *
 */

package syscall

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/env"
	dcUtil "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/util"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/blog"
)

const (
	ExitErrorCode            = 99
	DevOPSProcessTreeKillKey = "DEVOPS_DONT_KILL_PROCESS_TREE"
)

// RunServer run the detached server
func RunServer(command string) error {
	caller, options := GetCallerAndOptions()
	cmd := exec.Command(
		caller,
		options,
		command,
	)
	cmd.SysProcAttr = GetSysProcAttr()
	cmd.Dir = dcUtil.GetRuntimeDir()

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", DevOPSProcessTreeKillKey, "true"))

	blog.Infof("syscall: ready to run cmd [%s]", cmd.String())

	if err := cmd.Start(); err != nil {
		blog.Errorf("syscall: run server error: %v", err)
		return err
	}

	return nil
}

// GetSysProcAttr set process group id to a new id,
// in case of the signals sent to the caller affect the process as well
func GetSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
}

// GetCallerAndOptions return the caller and options in unix
func GetCallerAndOptions() (string, string) {
	return "/bin/bash", "-c"
}

// Sandbox describe the handler to build up an isolated execution environment
type Sandbox struct {
	Ctx    context.Context
	Env    *env.Sandbox
	Dir    string
	User   user.User
	Stdout io.Writer
	Stderr io.Writer

	spa *syscall.SysProcAttr
}

// Fork return a new sandbox which inherits from current one
func (s *Sandbox) Fork() *Sandbox {
	return &Sandbox{
		Ctx:    s.Ctx,
		Env:    s.Env,
		Dir:    s.Dir,
		Stdout: s.Stdout,
		Stderr: s.Stderr,
	}
}

// GetDir return the running dir
func (s *Sandbox) GetDir() string {
	if s.Dir != "" {
		return s.Dir
	}

	p, _ := os.Getwd()
	return p
}

// GetAbsPath return the abs path related to current running dir
func (s *Sandbox) GetAbsPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(s.GetDir(), path)
}

// ExecScripts run the scripts
func (s *Sandbox) ExecScripts(src string) (int, error) {
	caller, options := GetCallerAndOptions()
	return s.ExecCommand(caller, options, src)
}

func (s *Sandbox) ExecScriptsRaw(src string) (int, error) {
	return 1, fmt.Errorf("not support")
}

func (s *Sandbox) ExecRawByFile(bt, name string, arg ...string) (int, error) {
	return 1, fmt.Errorf("not support")
}

// ExecScriptsWithMessage run the scripts and return the output
func (s *Sandbox) ExecScriptsWithMessage(src string) (int, []byte, []byte, error) {
	caller, options := GetCallerAndOptions()
	return s.ExecCommandWithMessage(caller, options, src)
}

// StartScripts run the scripts
func (s *Sandbox) StartScripts(src string) (*exec.Cmd, error) {
	caller, options := GetCallerAndOptions()
	return s.StartCommand(caller, options, src)
}

// ExecCommandWithMessage run the commands and get the stdout and stderr
func (s *Sandbox) ExecCommandWithMessage(name string, arg ...string) (int, []byte, []byte, error) {
	var outBuf, errBuf bytes.Buffer
	s.Stdout = &outBuf
	s.Stderr = &errBuf

	code, err := s.ExecCommand(name, arg...)
	if err != nil && code != ExitErrorCode && len(errBuf.Bytes()) == 0 {
		return code, outBuf.Bytes(), []byte(err.Error()), err
	}

	return code, outBuf.Bytes(), errBuf.Bytes(), err
}

func isWithRootEnv() bool {
	envstr := env.GetEnv(env.KeyWorkerWithRootEnv)
	blog.Errorf("got env [%s] with key [%s]", envstr, env.KeyWorkerWithRootEnv)
	return envstr != ""
}

var (
	cmdsWithRootEnv = []string{
		"UbaAgent",
	}
)

func shouldUseRootEnv(name string) bool {
	for _, cmd := range cmdsWithRootEnv {
		if strings.Contains(name, cmd) {
			return true
		}
	}

	return false
}

func getRootEnv() []string {
	// 使用 `su -` 命令来模拟 root 用户的登录环境
	cmd := exec.Command("su", "-", "root", "-c", "env")

	// 获取命令输出
	output, err := cmd.Output()
	if err != nil {
		blog.Errorf("Failed to execute command [%+v] withe error %v", *cmd, err)
		return nil
	}

	// 将输出转换为字符串并分割成行
	envVars := strings.Split(string(output), "\n")
	blog.Infof("got root env [%+v]", envVars)

	return envVars
}

// ExecCommand run the origin commands
func (s *Sandbox) ExecCommand(name string, arg ...string) (int, error) {
	if s.Env == nil {
		s.Env = env.NewSandbox(os.Environ())
	}

	if s.User.Username == "" {
		if u, _ := user.Current(); u != nil {
			s.User = *u
		}
	}

	if s.Stdout == nil {
		s.Stdout = os.Stdout
	}
	if s.Stderr == nil {
		s.Stderr = os.Stderr
	}

	var err error
	var res string

	// if not relative path find the command in PATH
	if !strings.HasPrefix(name, ".") {
		res, err = s.LookPath(name)
		if err == nil {
			name = res
		}
	}

	var cmd *exec.Cmd
	if s.Ctx != nil {
		cmd = exec.CommandContext(s.Ctx, name, arg...)
	} else {
		cmd = exec.Command(name, arg...)
	}

	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	if isWithRootEnv() || shouldUseRootEnv(name) {
		rootEnv := getRootEnv()
		if rootEnv != nil {
			cmd.Env = rootEnv
		} else {
			cmd.Env = s.Env.Source()
		}
	} else {
		cmd.Env = s.Env.Source()
	}
	cmd.Dir = s.Dir
	cmd.SysProcAttr = s.spa

	// 错误等到stdout和stderr都初始化完, 再处理
	if err != nil {
		_, _ = s.Stderr.Write([]byte(fmt.Sprintf("run command failed: %v ,try relative path cmd\n", err.Error())))
		//return -1, err
	}

	blog.Debugf("syscall: cmd of [%+v] start", *cmd)
	defer blog.Debugf("syscall: cmd of [%+v] finished", *cmd)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus(), err
			}
		}
		return ExitErrorCode, err
	}
	return 0, nil
}

// StartCommand start the origin commands
func (s *Sandbox) StartCommand(name string, arg ...string) (*exec.Cmd, error) {
	if s.Env == nil {
		s.Env = env.NewSandbox(os.Environ())
	}

	if s.User.Username == "" {
		if u, _ := user.Current(); u != nil {
			s.User = *u
		}
	}

	if s.Stdout == nil {
		s.Stdout = os.Stdout
	}
	if s.Stderr == nil {
		s.Stderr = os.Stderr
	}

	var err error
	// if not relative path find the command in PATH
	if !strings.HasPrefix(name, ".") {
		name, err = s.LookPath(name)
	}

	var cmd *exec.Cmd
	if s.Ctx != nil {
		cmd = exec.CommandContext(s.Ctx, name, arg...)
	} else {
		cmd = exec.Command(name, arg...)
	}

	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	cmd.Env = s.Env.Source()
	cmd.Dir = s.Dir
	cmd.SysProcAttr = s.spa

	// 错误等到stdout和stderr都初始化完, 再处理
	if err != nil {
		_, _ = s.Stderr.Write([]byte(fmt.Sprintf("run command failed: %v\n", err.Error())))
		return cmd, err
	}

	if err := cmd.Start(); err != nil {
		// if exitErr, ok := err.(*exec.ExitError); ok {
		// 	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		// 		return status.ExitStatus(), err
		// 	}
		// }
		// return ExitErrorCode, err
		blog.Errorf("syscall: failed to start cmd with error: %v", err)
		return cmd, err
	}

	return cmd, nil
}

// LookPath 根据sandbox中的env-PATH, 来取得正确的command-name路径
func (s *Sandbox) LookPath(file string) (string, error) {
	if filepath.IsAbs(file) {
		err := findExecutable(file)
		if err == nil {
			return file, nil
		}
		return "", fmt.Errorf("command %s not found", file)
	}
	path := s.Env.GetOriginEnv("PATH")
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			// Unix shell semantics: path element "" means "."
			dir = "."
		}
		path := filepath.Join(dir, file)
		if err := findExecutable(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("command %s not found", file)
}

func findExecutable(file string) error {
	d, err := os.Stat(file)
	if err != nil {
		return err
	}
	if m := d.Mode(); !m.IsDir() && m&0111 != 0 {
		return nil
	}
	return os.ErrPermission
}

// GetConsoleCP only implement for windows now
func GetConsoleCP() int {
	return 0
}

// AddPath2Env add path to env
func AddPath2Env(p string) {
	path := os.Getenv("PATH")
	newpath := fmt.Sprintf("%s:%s", p, path)
	os.Setenv("PATH", newpath)
}

func RedirectStderror(f string) error {
	file, err := os.OpenFile(f, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println(err)
		return err
	}

	if err = syscall.Dup2(int(file.Fd()), int(os.Stderr.Fd())); err != nil {
		fmt.Println(err)
		return err
	}

	return nil
}

func NeedSearchToolchain(input *env.Sandbox) bool {
	return false
}
