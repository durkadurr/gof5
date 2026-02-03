package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/kayrus/gof5/pkg/client"
	"github.com/kayrus/gof5/pkg/config"
)

var (
	Version = "dev"
	info    = fmt.Sprintf("gof5 %s compiled with %s for %s/%s", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
)

func fatal(err error) {
	if runtime.GOOS == "windows" {
		// Escalated privileges in windows opens a new terminal, and if there is an
		// error, it is impossible to see it. Thus we wait for user to press a button.
		log.Printf("%s, press enter to exit", err)
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}
	log.Fatal(err)
}

func daemonize(logFilePath string) (*os.File, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("daemon mode is not supported on Windows")
	}

	// Create log directory if needed
	dir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file in the parent (before forking)
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Fork the process
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("failed to start daemon process: %w", err)
	}

	// Parent process closes its copy of the log file and exits
	logFile.Close()
	os.Exit(0)
	return nil, nil
}

func writePIDFile(pidPath string) error {
	// Ensure the directory exists
	dir := filepath.Dir(pidPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}

	// Write the PID to file
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

func removePIDFile(pidPath string) {
	if err := os.Remove(pidPath); err != nil {
		log.Printf("Warning: failed to remove PID file: %s", err)
	}
}

func main() {
	var version bool
	var passwordFile string
	var removePassFile bool
	var logFilePath string
	var opts client.Options

	// Check if we're the daemon child process
	if os.Getenv("__GOF5_DAEMONIZED") == "1" {
		// Child process: read password from env var
		opts.Password = os.Getenv("__GOF5_PASSWORD")
		// Clear for security
		os.Setenv("__GOF5_PASSWORD", "")
		// Clear passwordFile to prevent trying to read it again
		passwordFile = ""
	}

	flag.StringVar(&opts.Server, "server", "", "")
	flag.StringVar(&opts.Username, "username", "", "")
	flag.StringVar(&opts.Password, "password", "", "")
	flag.StringVar(&passwordFile, "password-file", "", "Path to file containing password")
	flag.BoolVar(&removePassFile, "remove-password-file", false, "Delete password file immediately after reading")
	flag.StringVar(&opts.SessionID, "session", "", "Reuse a session ID")
	flag.StringVar(&opts.CACert, "ca-cert", "", "Path to a custom CA certificate")
	flag.StringVar(&opts.Cert, "cert", "", "Path to a user TLS certificate")
	flag.StringVar(&opts.Key, "key", "", "Path to a user TLS key")
	flag.StringVar(&opts.ConfigPath, "config", "", "Path to config file (default: ~/.gof5/config.yaml)")
	flag.BoolVar(&opts.CloseSession, "close-session", false, "Close HTTPS VPN session on exit")
	flag.BoolVar(&opts.Debug, "debug", false, "Show debug logs")
	flag.BoolVar(&opts.Sel, "select", false, "Select a server from available F5 servers")
	flag.IntVar(&opts.ProfileIndex, "profile-index", 0, "If multiple VPN profiles are found chose profile n")
	flag.BoolVar(&version, "version", false, "Show version and exit cleanly")
	flag.StringVar(&logFilePath, "log-file", "", "Path to log file for daemon mode (default: /tmp/gof5/<username>.log)")

	flag.Parse()

	if version {
		fmt.Println(info)
		os.Exit(0)
	}

	if opts.ProfileIndex < 0 {
		fatal(fmt.Errorf("profile-index cannot be negative"))
	}

	if err := checkPermissions(); err != nil {
		fatal(err)
	}

	if flag.NArg() > 0 {
		if err := client.UrlHandlerF5Vpn(&opts, flag.Arg(0)); err != nil {
			fatal(err)
		}
	}

	// Read config before daemonizing so we can check the daemon flag
	cfg, err := config.ReadConfig(opts.Debug, opts.ConfigPath)
	if err != nil {
		fatal(err)
	}
	opts.Config = *cfg

	// Load password from file or environment variable if not provided via flag
	// Skip if already set from daemon env var
	if opts.Password == "" {
		if passwordFile != "" {
			data, err := os.ReadFile(passwordFile)
			if err != nil {
				fatal(fmt.Errorf("failed to read password file: %w", err))
			}
			opts.Password = strings.TrimSpace(string(data))
		} else if envPassword := os.Getenv("GOF5_PASSWORD"); envPassword != "" {
			opts.Password = envPassword
		}
	}

	// Get current user for PID/log file paths
	usr, err := user.Current()
	if err != nil {
		fatal(fmt.Errorf("failed to get current user: %w", err))
	}

	// Set up PID file path
	pidPath := filepath.Join("/tmp", "gof5", usr.Username+".pid")

	// Write PID file and schedule removal on exit
	if err := writePIDFile(pidPath); err != nil {
		fatal(err)
	}
	defer removePIDFile(pidPath)

	// Check if daemon mode is enabled (skip if already daemonized)
	if opts.Daemon && os.Getenv("__GOF5_DAEMONIZED") != "1" {
		if opts.Password == "" {
			fatal(fmt.Errorf("password is required for daemon mode; use --password, --password-file, or GOF5_PASSWORD environment variable"))
		}

		// Set environment variables for child process
		os.Setenv("__GOF5_PASSWORD", opts.Password)
		os.Setenv("__GOF5_DAEMONIZED", "1")

		// Delete password file if it exists and remove option is set
		if passwordFile != "" && removePassFile {
			if err := os.Remove(passwordFile); err != nil {
				log.Printf("Warning: failed to remove password file: %s", err)
			}
		}

		// Set default log file path if not specified
		if logFilePath == "" {
			logFilePath = filepath.Join("/tmp", "gof5", usr.Username+".log")
		}

		logFile, err := daemonize(logFilePath)
		if err != nil {
			fatal(err)
		}
		// We're now in the child process (daemon)
		// Redirect log output to the log file
		log.SetOutput(logFile)
		// Also redirect stderr for future error output
		syscall.Dup2(int(logFile.Fd()), int(os.Stderr.Fd()))

		// Rewrite PID file with child's PID
		if err := writePIDFile(pidPath); err != nil {
			log.Printf("Warning: failed to rewrite PID file: %s", err)
		}
	}

	if err := client.Connect(&opts); err != nil {
		fatal(err)
	}
}
