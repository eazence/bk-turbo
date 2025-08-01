/*
 * Copyright (c) 2021 THL A29 Limited, a Tencent company. All rights reserved
 *
 * This source code file is licensed under the MIT License, you may obtain a copy of the License at
 *
 * http://opensource.org/licenses/MIT
 *
 */

package command

import (
	"fmt"
	"os"
	"runtime"

	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/booster/pkg"
	dcUtil "github.com/TencentBlueKing/bk-turbo/src/backend/booster/bk_dist/common/util"
	"github.com/TencentBlueKing/bk-turbo/src/backend/booster/common/version"

	commandCli "github.com/urfave/cli"
)

// define const vars
const (
	FlagProjectID              = "project_id"
	FlagBuildID                = "build_id"
	FlagArgs                   = "args"
	FlagGccVersion             = "gcc_version"
	FlagBazel                  = "bazel"
	FlagBazelPlus              = "bazel_plus"
	FlagBazel4Plus             = "bazel4_plus"
	FlagBazelNoLauncher        = "bazel_no_launcher"
	FlagLauncher               = "launcher"
	FlagLog                    = "log"
	FlagLogDir                 = "log_dir"
	FlagLogToConsole           = "log_to_console"
	FlagExecutorLog            = "executor_log"
	FlagTest                   = "test"
	FlagCommandPath            = "command_path"
	FlagLimit                  = "limit"
	FlagNoLocal                = "no_local"
	FlagLocal                  = "local"
	FlagDegraded               = "degraded"
	FlagSaveCode               = "save_code"
	FlagJobs                   = "jobs"
	FlagMaxJobs                = "max_jobs"
	FlagPresetJobs             = "preset_jobs"
	FlagMaxDegradedJobs        = "max_degraded_jobs"
	FlagMaxLocalTotalJobs      = "max_local_total_jobs"
	FlagMaxLocalExeJobs        = "max_local_exe_jobs"
	FlagMaxLocalPreJobs        = "max_local_pre_jobs"
	FlagMaxLocalPostJobs       = "max_local_post_jobs"
	FlagHook                   = "hook"
	FlagBoosterType            = "booster_type"
	FlagHookConfig             = "hook_config"
	FlagHookPreloadLib         = "hook_preload_library"
	FlagAdditionFile           = "addition_file"
	FlagDashboard              = "dashboard"
	FlagWorkerList             = "worker_list"
	FlagCheckMd5               = "check_md5"
	FlagOutputEnvJSONFile      = "output_env_json_file"
	FlagOutputEnvSourceFile    = "output_env_source_file"
	FlagCommitSuicide          = "commit_suicide"
	FlagToolChainJSONFile      = "tool_chain_json_file"
	FlagSearchToolchain        = "search_toolchain"
	FlagBatchMode              = "batch_mode"
	FlagDirectives             = "directives"
	FlagGlobalSlots            = "global_slots"
	FlagSudoController         = "sudo_controller"
	FlagIOTimeoutSecs          = "io_timeout_secs"
	FlagPump                   = "pump"
	FlagPumpDisableMacro       = "pump_disable_macro"
	FlagPumpIncludeSysHeader   = "pump_include_sys_header"
	FlagPumpCheck              = "pump_check"
	FlagPumpCache              = "pump_cache"
	FlagPumpCacheDir           = "pump_cache_dir"
	FlagPumpCacheSizeMaxMB     = "pump_cache_size_max_MB"
	FlagPumpCacheRemoveAll     = "pump_cache_remove_all"
	FlagPumpBlackList          = "pump_black_list"
	FlagPumpMinActionNum       = "pump_min_action_num"
	FlagPumpDisableStatCache   = "pump_disable_stat_cache"
	FlagPumpSearchLink         = "pump_search_link"
	FlagPumpSearchLinkFile     = "pump_search_link_file"
	FlagPumpSearchLinkDir      = "pump_search_link_dir"
	FlagPumpLstatByDir         = "pump_lstat_by_dir"
	FlagPumpCorrectCap         = "pump_correct_cap"
	FlagForceLocalList         = "force_local_list"
	FlagNoWork                 = "no_work"
	FlagControllerNoWait       = "controller_no_wait"
	FlagControllerRemainTime   = "controller_remain_time"
	FlagServer                 = "server"
	FlagResultCacheList        = "result_cache_list"
	FlagWorkerSideCache        = "worker_side_cache"
	FlagLocalRecord            = "local_record"
	FlagWriteMemroMemroy       = "write_memory"
	FlagIdleKeepSecs           = "idle_keep_secs"
	FlagResourceTimeoutSecs    = "resource_timeout_secs"
	FlagLocalIdleCPUPercent    = "use_local_cpu_percent"
	FlagDisableFileLock        = "disable_file_lock"
	FlagAutoResourceMgr        = "auto_resource_mgr"
	FlagResIdleSecsForFree     = "res_idle_secs_for_free"
	FlagSendCork               = "send_cork"
	FlagSendFileMemoryLimit    = "send_file_memory_limit"
	FlagSendMemoryCache        = "send_memory_cache"
	FlagNetErrorLimit          = "net_error_limit"
	FlagRemoteRetryTimes       = "remote_retry_times"
	FlagEnableLink             = "enable_link"
	FlagEnableLib              = "enable_lib"
	FlagLongTCP                = "long_tcp"
	FlagUseDefaultWorker       = "use_default_worker"
	FlagDynamicPort            = "dynamic_port"
	FlagWorkerOfferSlot        = "worker_offer_slot"
	FlagCleanTmpFilesDayAgo    = "clean_tmp_files_day_ago"
	FlagIgnoreHttpStatus       = "ignore_http_status"
	FlagResultCacheType        = "result_cache_type"
	FlagResultCacheTriggleSecs = "result_cache_triggle_secs"
	FlagResultCacheIndexNum    = "result_cache_index_num"
	FlagResultCacheFileNum     = "result_cache_file_num"
	FlagPreferLocal            = "prefer_local"

	EnvBuildIDOld  = "TURBO_PLAN_BUILD_ID"
	EnvBuildID     = "TBS_BUILD_ID"
	EnvProjectID   = "TBS_PROJECT_ID"
	EnvBoosterType = "TBS_BOOSTER_TYPE"

	exitCodeFile = "bk_origin_exit_code"

	linuxDefaultPreloadLibrary = "/usr/lib64/bk-hook.so"
)

var (
	ServerNecessary = ""
)

// Run main entrance
func Run(ct ClientType) {
	if err := run(ct); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		switch err {
		case pkg.ErrCompile:
			os.Exit(3)
		case pkg.ErrNoLocal:
			os.Exit(4)
		default:
			os.Exit(1)
		}
	}
}

func run(ct ClientType) error {
	return GetApp(ct).Run(os.Args)
}

var (
	commonFlags = []commandCli.Flag{
		commandCli.StringFlag{
			Name:  "log",
			Usage: "log level to print some information",
		},
		commandCli.StringFlag{
			Name:  "log_dir",
			Usage: "log dir to save log files",
		},
		commandCli.StringFlag{
			Name:  "log_to_console",
			Usage: "whether log to console, true for default, set false to disable this",
		},
		commandCli.StringFlag{
			Name:  "executor_log",
			Usage: "executor log level to print some information",
			Value: dcUtil.PrintNothing.String(),
		},
		commandCli.StringFlag{
			Name:  "project_id, p",
			Usage: "project ID of this build, it is necessary",
		},
		commandCli.StringFlag{
			Name:  "booster_type, bt",
			Usage: "booster build type",
		},
		commandCli.StringFlag{
			Name:  "args, a",
			Usage: "flags and args that will be pass-through",
		},
		commandCli.BoolFlag{
			Name:  "test, t",
			Usage: "connect to test environment",
		},
		commandCli.IntFlag{
			Name:  "limit, l",
			Usage: "limit the jobs per remote instance",
		},
		commandCli.BoolFlag{
			Name:  "local",
			Usage: "no applying resources, degraded all tasks to local compiling",
		},
		commandCli.BoolFlag{
			Name:  "degraded",
			Usage: "degraded this task to local compiling",
		},
		commandCli.BoolFlag{
			Name:  "no_local",
			Usage: "never degraded to local compiling, or exit with code 4",
		},
		commandCli.BoolFlag{
			Name:  "save_code, s",
			Usage: "save the original command exit code in file bk_origin_exit_code",
		},
		commandCli.IntFlag{
			Name:  "jobs",
			Usage: "parallel jobs for working, it will be used if it is specified and less than max_jobs",
		},
		commandCli.IntFlag{
			Name: "max_jobs",
			Usage: "max parallel jobs for working, if the jobs given by server is larger, " +
				"then use the max_jobs instead",
		},
		commandCli.IntFlag{
			Name:  "preset_jobs",
			Usage: "preset jobs define the parallel jobs for working while applying resource failed at the first time, ",
		},
		commandCli.IntFlag{
			Name: "max_degraded_jobs",
			Usage: "max parallel jobs for working when whole task degraded to local, if local cpu number is larger, " +
				"then use the max_degraded_jobs instead",
		},
		commandCli.IntFlag{
			Name: "max_local_total_jobs",
			Usage: "max parallel jobs for total local works, matters the max jobs of max_local_exe_jobs, " +
				"max_local_pre_jobs and max_local_post_jobs",
		},
		commandCli.IntFlag{
			Name:  "max_local_exe_jobs",
			Usage: "max parallel jobs for local execute works, matters the max fallback working concurrency in client",
		},
		commandCli.IntFlag{
			Name:  "max_local_pre_jobs",
			Usage: "max parallel jobs for local pre works, matters the max pre-process concurrency in client",
		},
		commandCli.IntFlag{
			Name:  "max_local_post_jobs",
			Usage: "max parallel jobs for local post works, matters the max post-process concurrency in client",
		},
		commandCli.BoolFlag{
			Name:  "hook",
			Usage: "enable hook mode",
		},
		commandCli.StringFlag{
			Name:  "hook_config",
			Usage: "hook config file, which contains json data",
		},
		commandCli.StringFlag{
			Name:  "hook_preload_library",
			Usage: "hook preload library file",
			Value: linuxDefaultPreloadLibrary,
		},
		commandCli.StringSliceFlag{
			Name:  "addition_file, af",
			Usage: "send addition file to every worker before build begins",
		},
		commandCli.BoolFlag{
			Name:  "dashboard, d",
			Usage: "html server for work dashboard",
		},
		commandCli.StringSliceFlag{
			Name:  "worker_list, wl",
			Usage: "set worker list to use",
		},
		commandCli.BoolFlag{
			Name:  "check_md5, cm",
			Usage: "enable md5 check when transfer files",
		},
		commandCli.StringSliceFlag{
			Name:  "output_env_json_file",
			Usage: "output json file to save bk envs",
		},
		commandCli.StringSliceFlag{
			Name:  "output_env_source_file",
			Usage: "output source file to save bk envs",
		},
		commandCli.BoolFlag{
			Name:  "commit_suicide",
			Usage: "enable commit suicide when become orphan process",
		},
		commandCli.StringFlag{
			Name:  "tool_chain_json_file",
			Usage: "json file to describe tool chain",
		},
		commandCli.BoolFlag{
			Name:  "search_toolchain",
			Usage: "automatically search for toolchain based on command",
		},
		commandCli.BoolFlag{
			Name:  "batch_mode, bm",
			Usage: "batch mode for booster, multi booster in the same projectID will use one same workID",
		},
		commandCli.BoolFlag{
			Name:  "directives",
			Usage: "to support -fdirectives-only when preprocess",
		},
		commandCli.BoolFlag{
			Name:  "global_slots, gs",
			Usage: "share global slots with other boosters",
		},
		commandCli.BoolFlag{
			Name:  "sudo_controller, sc",
			Usage: "sudo start controller if need",
		},
		commandCli.IntFlag{
			Name:  "io_timeout_secs",
			Usage: "max wait seconds for read/write with worker",
		},
		commandCli.BoolFlag{
			Name:  "pump",
			Usage: "do pre-process in pump mode",
		},
		commandCli.BoolFlag{
			Name:  "pump_include_sys_header",
			Usage: "include system header in pump mode",
		},
		commandCli.BoolFlag{
			Name:  "pump_disable_macro",
			Usage: "disable macro check in pump mode",
		},
		commandCli.BoolFlag{
			Name:  "pump_check",
			Usage: "check pre-process in pump mode",
		},
		commandCli.BoolFlag{
			Name:  "pump_cache",
			Usage: "use cached depend files in pump mode",
		},
		commandCli.StringFlag{
			Name:  "pump_cache_dir",
			Usage: "specify the pump cache dir",
		},
		commandCli.IntFlag{
			Name:  "pump_cache_size_max_MB",
			Usage: "max pump cache size(MB)",
		},
		commandCli.BoolFlag{
			Name:  "pump_cache_remove_all",
			Usage: "remove all of pump cache files",
		},
		commandCli.StringSliceFlag{
			Name:  "pump_black_list, pbl",
			Usage: "action in this list will not use pump",
		},
		commandCli.IntFlag{
			Name:  "pump_min_action_num",
			Usage: "do not use pump if total actions less this",
		},
		commandCli.BoolFlag{
			Name:  "pump_disable_stat_cache",
			Usage: "whether disable pump depend file stat info cache, default is false",
		},
		commandCli.BoolFlag{
			Name:  "pump_search_link",
			Usage: "whether search link files",
		},
		commandCli.StringFlag{
			Name:  "pump_search_link_file",
			Usage: "specify the file which record link result",
		},
		commandCli.StringSliceFlag{
			Name:  "pump_search_link_dir",
			Usage: "specify the dir where to search link files",
		},
		commandCli.BoolFlag{
			Name:  "pump_lstat_by_dir",
			Usage: "whether get file stat info by search dir",
		},
		commandCli.BoolFlag{
			Name:  "pump_correct_cap",
			Usage: "whether correct capitalization when save pump depend file list",
		},
		commandCli.StringSliceFlag{
			Name:  "force_local_list, fll",
			Usage: "key list which will be force executed locally",
		},
		commandCli.BoolFlag{
			Name:  "no_work, nw",
			Usage: "do not register work, just do the handler init",
		},
		commandCli.BoolFlag{
			Name:  "controller_no_wait",
			Usage: "tell controller no wait if no task running",
		},
		commandCli.IntFlag{
			Name:  "controller_remain_time",
			Usage: "max wait seconds for controller idle running",
		},
		commandCli.StringFlag{
			Name:  "server",
			Usage: "specify the server address for booster",
		},
		commandCli.StringSliceFlag{
			Name:  "result_cache_list, rcl",
			Usage: "specify the result cache list for booster",
		},
		commandCli.BoolFlag{
			Name:  "worker_side_cache, wsc",
			Usage: "check file cache in worker side before sending",
		},
		commandCli.BoolFlag{
			Name:  "local_record, lr",
			Usage: "record and store jobs' performance in local and make some strategy in later work",
		},
		commandCli.BoolFlag{
			Name:  "write_memory",
			Usage: "to support write preprocessed file to memory, default to temp dir of system drive",
		},
		commandCli.IntFlag{
			Name:  "idle_keep_secs",
			Usage: "max wait seconds before release idle resource",
		},
		commandCli.IntFlag{
			Name:  "resource_timeout_secs",
			Usage: "max seconds while waiting for apply resource",
		},
		commandCli.IntFlag{
			Name:  "use_local_cpu_percent",
			Usage: "how many local idle cpu will be used to execute tasks(0~100), default 0",
		},
		commandCli.BoolFlag{
			Name:  "disable_file_lock",
			Usage: "whether need disable file lock when launch program, false by default",
		},
		commandCli.BoolFlag{
			Name:  "auto_resource_mgr",
			Usage: "whether auto free and apply resource while task is running",
		},
		commandCli.IntFlag{
			Name:  "res_idle_secs_for_free",
			Usage: "free this resource if oever this idle seconds, only used when auto_resource_mgr is true",
		},
		commandCli.BoolFlag{
			Name:  "send_cork",
			Usage: "send files like tcp cork",
		},
		commandCli.Int64Flag{
			Name:  "send_file_memory_limit",
			Usage: "set send file memory limit",
		},
		commandCli.BoolFlag{
			Name:  "send_memory_cache",
			Usage: "send files with memory cache",
		},
		commandCli.IntFlag{
			Name:  "net_error_limit",
			Usage: "disable a remote worker which's continuous net errors reach this limit",
		},
		commandCli.IntFlag{
			Name:  "remote_retry_times",
			Usage: "retry a remote failed task for serveral times before degrad it to local",
		},
		commandCli.BoolFlag{
			Name:  "enable_link",
			Usage: "enable remote link.exe",
		},
		commandCli.BoolFlag{
			Name:  "enable_lib",
			Usage: "enable remote lib.exe",
		},
		commandCli.BoolFlag{
			Name:  "long_tcp",
			Usage: "whether connect to worker with long tcp connection",
		},
		commandCli.BoolFlag{
			Name:  "use_default_worker",
			Usage: "use default worker if worker id is empty",
		},
		commandCli.BoolFlag{
			Name:  "dynamic_port",
			Usage: "controller will listen dynamic port if true",
		},
		commandCli.BoolFlag{
			Name:  "worker_offer_slot",
			Usage: "controller will get remote slot by worker offer",
		},
		commandCli.IntFlag{
			Name:  "clean_tmp_files_day_ago",
			Usage: "clean tmp files which modify time before this days, default is 1",
		},
		commandCli.BoolFlag{
			Name:  "ignore_http_status",
			Usage: "tasks will be executed even local http connection disconnected when this flag set",
		},
		commandCli.IntFlag{
			Name:  "result_cache_type",
			Usage: "use which result cache type, 0:no cache, 1:local cache, 2:remote cache, 3:both",
		},
		commandCli.IntFlag{
			Name:  "result_cache_triggle_secs",
			Usage: "only remote execute time(seconds) great than this will triggle result cache",
		},
		commandCli.IntFlag{
			Name:  "result_cache_index_num",
			Usage: "specify index number for local result cache",
		},
		commandCli.IntFlag{
			Name:  "result_cache_file_num",
			Usage: "specify file number for local result cache",
		},
		commandCli.BoolFlag{
			Name:  "prefer_local",
			Usage: "if true, controller will try to use local first",
		},
	}
)

// GetApp get app by type
func GetApp(ct ClientType) *commandCli.App {
	client := commandCli.NewApp()
	client.Name = ct.Name()
	client.Usage = ct.Usage()
	client.Version = fmt.Sprintf("Version:   %s\n\t Tag:       %s\n\t BuildTime: %s\n\t GitHash:   %s",
		version.Version, version.Tag, version.BuildTime, version.GitHash)

	client.Flags = commonFlags

	switch ct {
	case ClientBKBooster:
		client.Flags = append(client.Flags, []commandCli.Flag{
			commandCli.StringFlag{
				Name:  "build_id, b",
				Usage: "ID of this build",
			},
			// no recommend to use, use -lc instead
			commandCli.BoolFlag{
				Name:  "bazel, bz",
				Usage: "if the flag set, then consider the args as a pure bazel command",
			},
			commandCli.BoolFlag{
				Name: "bazel_plus, bzp",
				Usage: "if the flag set, then consider the args as a pure bazel command and keep the incremental " +
					"compilation works",
			},
			commandCli.BoolFlag{
				Name: "bazel4_plus, bz4p",
				Usage: "if the flag set, then consider the args as a pure bazel command and keep the incremental " +
					"compilation works with at least bazel 4.0",
			},
			commandCli.BoolFlag{
				Name: "bazel_no_launcher, bz4nl",
				Usage: "if the flag set, then consider the args as a pure bazel command and keep the incremental " +
					"compilation works with at least bazel 4.0; this will simplify bazel accelerate environment",
			},
			commandCli.BoolFlag{
				Name:  "launcher, lc",
				Usage: "if the flag set, then take launcher instead of calling executor directly",
			},
		}...)

		client.Action = boosterProcess

	default:
		client.Action = func(c *commandCli.Context) error {
			return fmt.Errorf("unknown client")
		}
	}

	// override the version printer
	commandCli.VersionPrinter = func(c *commandCli.Context) {
		fmt.Printf("GoVersion: %s\nVersion:   %s\nTag:       %s\nBuildTime: %s\nGitHash:   %s\n",
			runtime.Version(), version.Version, version.Tag, version.BuildTime, version.GitHash)
	}

	return client
}
