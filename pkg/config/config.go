package config

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/kayrus/gof5/pkg/util"

	"gopkg.in/yaml.v2"
)

const (
	configDir  = ".gof5"
	configName = "config.yaml"
)

var (
	defaultDNSListenAddr = net.IPv4(127, 0, 0, 0xf5).To4()
	// BSD systems don't support listeniing on 127.0.0.1+N
	defaultBSDDNSListenAddr = net.IPv4(127, 0, 0, 1).To4()
	supportedDrivers        = []string{"wireguard", "pppd"}
)

func ReadConfig(debug bool, customConfigPath string) (*Config, error) {
	var err error
	var usr *user.User

	// resolve sudo user ID
	if id, sudoUID := os.Geteuid(), os.Getenv("SUDO_UID"); id == 0 && sudoUID != "" {
		usr, err = user.LookupId(sudoUID)
		if err != nil {
			log.Printf("failed to lookup user ID: %s", err)
			if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
				usr, err = user.Lookup(sudoUser)
				if err != nil {
					return nil, fmt.Errorf("failed to lookup user name: %s", err)
				}
			}
		}
	} else {
		// detect home directory
		usr, err = user.Current()
		if err != nil {
			return nil, fmt.Errorf("failed to detect home directory: %s", err)
		}
	}

	var configPath string
	var configFile string

	if customConfigPath != "" {
		// Use custom config path
		configFile = customConfigPath
		configPath = filepath.Dir(customConfigPath)
	} else {
		// Use default config path
		configPath = filepath.Join(usr.HomeDir, configDir)
		configFile = filepath.Join(configPath, configName)
	}

	var uid, gid int
	// windows preserves the original user parameters, no need to detect uid/gid
	if runtime.GOOS != "windows" {
		uid, err = strconv.Atoi(usr.Uid)
		if err != nil {
			return nil, fmt.Errorf("failed to convert %q UID to integer: %s", usr.Uid, err)
		}
		gid, err = strconv.Atoi(usr.Gid)
		if err != nil {
			return nil, fmt.Errorf("failed to convert %q GID to integer: %s", usr.Uid, err)
		}
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Printf("%q directory doesn't exist, creating...", configPath)
		if err := os.MkdirAll(configPath, 0700); err != nil {
			return nil, fmt.Errorf("failed to create %q config directory: %s", configPath, err)
		}
		// windows preserves the original user parameters, no need to chown
		if runtime.GOOS != "windows" {
			if err := os.Chown(configPath, uid, gid); err != nil {
				return nil, fmt.Errorf("failed to set an owner for the %q config directory: %s", configPath, err)
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get %q directory stat: %s", configPath, err)
	}

	cfg := &Config{}
	// read config file
	// if config doesn't exist, use defaults
	if raw, err := ioutil.ReadFile(configFile); err == nil {
		if err = yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("cannot parse %s file: %v", configFile, err)
		}
	} else {
		log.Printf("Cannot read config file: %s", err)
	}

	// set default driver
	if cfg.Driver == "" {
		cfg.Driver = "wireguard"
	}

	if cfg.Driver == "wireguard" {
		if err := checkWinTunDriver(); err != nil {
			return nil, err
		}
	}

	if cfg.Driver == "pppd" && runtime.GOOS == "windows" {
		return nil, fmt.Errorf("pppd driver is not supported in Windows")
	}

	if !util.StrSliceContains(supportedDrivers, cfg.Driver) {
		return nil, fmt.Errorf("%q driver is unsupported, supported drivers are: %q", cfg.Driver, supportedDrivers)
	}

	if cfg.ListenDNS == nil {
		switch runtime.GOOS {
		case "freebsd",
			"darwin":
			cfg.ListenDNS = defaultBSDDNSListenAddr
		default:
			cfg.ListenDNS = defaultDNSListenAddr
		}
	}

	cfg.Path = configPath

	// Always use ~/.gof5 for cookies regardless of custom config path
	cookiePath := filepath.Join(usr.HomeDir, configDir)
	if cookiePath != configPath {
		// Ensure the cookie directory exists when using custom config
		if _, err := os.Stat(cookiePath); os.IsNotExist(err) {
			log.Printf("%q directory doesn't exist, creating...", cookiePath)
			if err := os.MkdirAll(cookiePath, 0700); err != nil {
				return nil, fmt.Errorf("failed to create %q cookie directory: %s", cookiePath, err)
			}
			if runtime.GOOS != "windows" {
				if err := os.Chown(cookiePath, uid, gid); err != nil {
					return nil, fmt.Errorf("failed to set an owner for the %q cookie directory: %s", cookiePath, err)
				}
			}
		} else if err != nil {
			return nil, fmt.Errorf("failed to get %q directory stat: %s", cookiePath, err)
		}
	}
	cfg.CookiePath = cookiePath
	cfg.Uid = uid
	cfg.Gid = gid

	cfg.Debug = debug

	return cfg, nil
}
