package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/libtrust"
	"github.com/labring/sealos/pkg/client-go/kubernetes"
	yaml "gopkg.in/yaml.v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

var k8sClient kubernetes.Client

type Config struct {
	Server ServerConfig `yaml:"server"`
	Token  TokenConfig  `yaml:"token"`
}

// nolint:revive
type ServerConfig struct {
	ListenAddress string `yaml:"addr,omitempty"`
	PathPrefix    string `yaml:"path_prefix,omitempty"`

	MaxRequestsPerIP         int           `yaml:"max_requests_per_ip,omitempty"`
	MaxRequestsPerAccount    int           `yaml:"max_requests_per_account,omitempty"`
	ReqLimitersResetInterval time.Duration `yaml:"req_limiters_reset_interval,omitempty"`
	WhiteIPCidrList          []string      `yaml:"white_ip_cidr_list,omitempty"`
	WhiteUserList            []string      `yaml:"white_user_list,omitempty"`
}

type TokenConfig struct {
	Issuer     string `yaml:"issuer,omitempty"`
	CertFile   string `yaml:"certificate,omitempty"`
	KeyFile    string `yaml:"key,omitempty"`
	Expiration int64  `yaml:"expiration,omitempty"`

	publicKey  libtrust.PublicKey
	privateKey libtrust.PrivateKey
}

func validate(c *Config) error {
	if c.Server.ListenAddress == "" {
		return errors.New("server.addr is required")
	}
	if c.Server.PathPrefix != "" && !strings.HasPrefix(c.Server.PathPrefix, "/") {
		return errors.New("server.path_prefix must be an absolute path")
	}
	if c.Token.Issuer == "" {
		return errors.New("token.issuer is required")
	}
	if c.Token.Expiration <= 0 {
		return fmt.Errorf("expiration must be positive, got %d", c.Token.Expiration)
	}
	return nil
}

func loadCertAndKey(certFile string, keyFile string) (pk libtrust.PublicKey, prk libtrust.PrivateKey, err error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return
	}
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return
	}
	pk, err = libtrust.FromCryptoPublicKey(x509Cert.PublicKey)
	if err != nil {
		return
	}
	prk, err = libtrust.FromCryptoPrivateKey(cert.PrivateKey)
	return
}

const (
	DefaultMaxRequestsPerAccount    = 1000
	DefaultMaxRequestsPerIP         = 1000
	DefaultReqLimitersResetInterval = 1 * time.Hour
)

func LoadConfig(fileName string) (*Config, error) {
	contents, err := os.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %s", fileName, err)
	}
	c := &Config{}
	if err = yaml.Unmarshal(contents, c); err != nil {
		return nil, fmt.Errorf("could not parse config: %s", err)
	}
	// set default ListenAddress
	if c.Server.ListenAddress == "" {
		c.Server.ListenAddress = ":5001"
	}
	if c.Server.MaxRequestsPerIP == 0 {
		c.Server.MaxRequestsPerIP = DefaultMaxRequestsPerIP
	}
	if c.Server.MaxRequestsPerAccount == 0 {
		c.Server.MaxRequestsPerAccount = DefaultMaxRequestsPerAccount
	}
	if c.Server.ReqLimitersResetInterval == 0 {
		c.Server.ReqLimitersResetInterval = DefaultReqLimitersResetInterval
	}
	if err = validate(c); err != nil {
		return nil, fmt.Errorf("invalid config: %s", err)
	}
	tokenConfigured := false
	if c.Token.CertFile != "" || c.Token.KeyFile != "" {
		// Check for partial configuration.
		if c.Token.CertFile == "" || c.Token.KeyFile == "" {
			return nil, fmt.Errorf("failed to load token cert and key: both were not provided")
		}
		publicKey, privateKey, err := loadCertAndKey(c.Token.CertFile, c.Token.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load token cert and key: %s", err)
		}
		c.Token.publicKey, c.Token.privateKey = publicKey, privateKey
		tokenConfigured = true
	}
	if !tokenConfigured {
		return nil, fmt.Errorf("failed to load token cert and key: none provided")
	}
	// setup k8sClient by using controller-runtime
	k8sClient, err = kubernetes.NewKubernetesClientByConfig(ctrl.GetConfigOrDie())
	if err != nil {
		return nil, err
	}
	return c, nil
}