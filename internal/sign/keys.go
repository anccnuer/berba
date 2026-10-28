package sign

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

type Key struct {
	Private *rsa.PrivateKey
	Cert    *x509.Certificate
	CertDER []byte
	CertPEM []byte
	KeyPEM  []byte
}

func Generate(path string) (*Key, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serialMax := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialMax)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "guiapp",
			Organization: []string{"guiapp"},
		},
		Issuer: pkix.Name{
			CommonName:   "guiapp",
			Organization: []string{"guiapp"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(30, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	var buf []byte
	buf = append(buf, keyPEM...)
	buf = append(buf, certPEM...)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return nil, err
	}
	return &Key{Private: priv, Cert: cert, CertDER: certDER, CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

func Load(path string) (*Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var priv *rsa.PrivateKey
	var certDER []byte
	var keyPEM, certPEM []byte
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY":
			k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("解析私钥失败: %w", err)
			}
			rk, ok := k.(*rsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("仅支持 RSA 私钥")
			}
			priv = rk
			keyPEM = pem.EncodeToMemory(block)
		case "RSA PRIVATE KEY":
			k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("解析私钥失败: %w", err)
			}
			priv = k
			keyPEM = pem.EncodeToMemory(block)
		case "CERTIFICATE":
			certDER = block.Bytes
			certPEM = pem.EncodeToMemory(block)
		}
	}
	if priv == nil || certDER == nil {
		return nil, fmt.Errorf("密钥文件缺少私钥或证书: %s", path)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	return &Key{Private: priv, Cert: cert, CertDER: certDER, CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

func LoadOrCreate(path string) (*Key, bool, error) {
	if _, err := os.Stat(path); err == nil {
		k, err := Load(path)
		return k, false, err
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	k, err := Generate(path)
	if err != nil {
		return nil, false, err
	}
	return k, true, nil
}

func (k *Key) Fingerprint() string {
	sum := sha256.Sum256(k.CertDER)
	hexStr := hex.EncodeToString(sum[:])
	var b []byte
	for i := 0; i < len(hexStr); i += 2 {
		if len(b) > 0 {
			b = append(b, ':')
		}
		b = append(b, hexStr[i], hexStr[i+1])
	}
	return string(b)
}
