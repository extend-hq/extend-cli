package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func resolved() (v, c, d string) {
	v, c, d = Version, Commit, Date
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				v = info.Main.Version
			}
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					if c == "" {
						c = s.Value
					}
				case "vcs.time":
					if d == "" {
						d = s.Value
					}
				}
			}
		}
	}
	return v, c, d
}

func Short() string {
	v, c, _ := resolved()
	if c != "" && len(c) >= 7 {
		return v + " (" + c[:7] + ")"
	}
	return v
}

func String() string {
	v, c, d := resolved()
	out := fmt.Sprintf("extend %s", v)
	if c != "" {
		short := c
		if len(short) > 12 {
			short = short[:12]
		}
		out += " (" + short
		if d != "" {
			out += ", " + d
		}
		out += ")"
	} else if d != "" {
		out += " (" + d + ")"
	}
	out += "\n  " + runtime.GOOS + "/" + runtime.GOARCH + " " + runtime.Version()
	return out
}
