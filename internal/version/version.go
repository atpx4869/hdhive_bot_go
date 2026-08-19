package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version 信息，构建时通过 ldflags 注入
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// Info 版本信息
type Info struct {
	Version   string
	GitCommit string
	BuildTime string
	GoVersion string
	OS        string
	Arch      string
	Module    *debug.Module
}

// Get 获取版本信息
func Get() Info {
	info := Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	// 尝试获取模块信息
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.Module = &bi.Main
	}

	return info
}

// String 返回格式化的版本字符串
func (i Info) String() string {
	s := fmt.Sprintf("hdhive-bot-go %s (%s) built at %s", i.Version, i.GitCommit, i.BuildTime)
	s += fmt.Sprintf("\n  Go: %s, OS: %s, Arch: %s", i.GoVersion, i.OS, i.Arch)
	if i.Module != nil {
		s += fmt.Sprintf("\n  Module: %s %s", i.Module.Path, i.Module.Version)
	}
	return s
}

// Short 返回简短版本字符串
func (i Info) Short() string {
	return fmt.Sprintf("%s (%s)", i.Version, i.GitCommit)
}
