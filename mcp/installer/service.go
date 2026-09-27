package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	launchdLabel = "com.team-memory.mcp"
	systemdUnit  = "team-memory-mcp.service"
)

// ServicePATH is baked into the service definition: launchd and systemd
// start services with a minimal PATH, so the `claude` CLI (and gh, for
// token_source=gh) would otherwise be missing.
func ServicePATH(bin string) string {
	dirs := []string{filepath.Dir(bin)}
	for _, tool := range []string{"claude", "gh"} {
		if p, err := exec.LookPath(tool); err == nil {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func LaunchdPlist(bin string, port int, logDir, path string) string {
	esc := func(s string) string {
		r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
		return r.Replace(s)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>--port</string>
    <string>%d</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, launchdLabel, esc(bin), port, esc(path), esc(filepath.Join(logDir, "service.log")), esc(filepath.Join(logDir, "service.log")))
}

func SystemdUnit(bin string, port int, path string) string {
	return fmt.Sprintf(`[Unit]
Description=team-memory (shared AI memory: web UI, sync, background capture)
After=network-online.target

[Service]
ExecStart=%s serve --port %d
Restart=on-failure
RestartSec=5s
Environment=PATH=%s

[Install]
WantedBy=default.target
`, strconv.Quote(bin), port, path)
}

func run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallService installs and starts the always-on background service.
func InstallService(p Paths, port int, logDir string) (string, error) {
	path := ServicePATH(p.Bin)
	switch runtime.GOOS {
	case "darwin":
		plist := filepath.Join(p.Home, "Library", "LaunchAgents", launchdLabel+".plist")
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return "", err
		}
		_ = os.MkdirAll(logDir, 0o700)
		if err := os.WriteFile(plist, []byte(LaunchdPlist(p.Bin, port, logDir, path)), 0o644); err != nil {
			return "", err
		}
		domain := "gui/" + strconv.Itoa(os.Getuid())
		_ = run("launchctl", "bootout", domain+"/"+launchdLabel)
		if err := run("launchctl", "bootstrap", domain, plist); err != nil {
			_ = run("launchctl", "unload", plist)
			if err2 := run("launchctl", "load", "-w", plist); err2 != nil {
				return "", fmt.Errorf("launchd: %v", err)
			}
		}
		return "launchd agent " + launchdLabel + " started (" + plist + ")", nil
	case "linux":
		dir := filepath.Join(p.Home, ".config", "systemd", "user")
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			dir = filepath.Join(v, "systemd", "user")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		unit := filepath.Join(dir, systemdUnit)
		if err := os.WriteFile(unit, []byte(SystemdUnit(p.Bin, port, path)), 0o644); err != nil {
			return "", err
		}
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return "", err
		}
		if err := run("systemctl", "--user", "enable", "--now", systemdUnit); err != nil {
			return "", err
		}
		_ = run("systemctl", "--user", "restart", systemdUnit)
		return "systemd user service " + systemdUnit + " started", nil
	}
	return "", fmt.Errorf("background service setup on %s is handled by install.ps1", runtime.GOOS)
}

// RemoveService stops and removes the background service, if present.
func RemoveService(p Paths) string {
	switch runtime.GOOS {
	case "darwin":
		plist := filepath.Join(p.Home, "Library", "LaunchAgents", launchdLabel+".plist")
		if _, err := os.Stat(plist); err != nil {
			return ""
		}
		_ = run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel)
		_ = run("launchctl", "unload", plist)
		os.Remove(plist)
		return "removed launchd agent"
	case "linux":
		unit := filepath.Join(p.Home, ".config", "systemd", "user", systemdUnit)
		if _, err := os.Stat(unit); err != nil {
			return ""
		}
		_ = run("systemctl", "--user", "disable", "--now", systemdUnit)
		os.Remove(unit)
		_ = run("systemctl", "--user", "daemon-reload")
		return "removed systemd user service"
	}
	return ""
}

// ServiceInstalled reports whether a background service definition exists.
func ServiceInstalled(p Paths) bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := os.Stat(filepath.Join(p.Home, "Library", "LaunchAgents", launchdLabel+".plist"))
		return err == nil
	case "linux":
		_, err := os.Stat(filepath.Join(p.Home, ".config", "systemd", "user", systemdUnit))
		return err == nil
	}
	return false
}
