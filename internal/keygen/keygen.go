package keygen

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Run(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	keyType := fs.String("t", "ed25519", "Key type: ed25519, rsa, ecdsa")

	fs.Usage = func() {
		fmt.Println("用法：keygen [选项] [用户名]")
		fmt.Println("  用户名：密钥文件的名称（默认：user）")
		fmt.Println("选项：")
		fs.PrintDefaults()
	}

	fs.Parse(args)

	name := "user"
	if fs.NArg() > 0 {
		name = fs.Arg(0)
	}

	if err := os.MkdirAll("keys", 0755); err != nil {
		panic(err)
	}

	switch strings.ToLower(*keyType) {
	case "rsa":
		generateRSA(name)
	case "ecdsa":
		generateECDSA(name)
	case "ed25519":
		generateEd25519(name)
	default:
		fmt.Printf("未知的密钥类型：%s。请使用 ed25519、rsa 或 ecdsa。\n", *keyType)
	}
}

func generateEd25519(name string) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	pubHex := hex.EncodeToString(pub)
	privHex := hex.EncodeToString(priv)

	pubFile := filepath.Join("keys", name+".pub")
	privFile := filepath.Join("keys", name+".key")

	saveFile(pubFile, []byte(pubHex))
	saveFile(privFile, []byte(privHex))

	fmt.Printf("已为 '%s' 生成 Ed25519 密钥\n", name)
}

func generateRSA(name string) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	// Save Private
	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	privPem := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})
	saveFile(filepath.Join("keys", name+".key"), privPem)

	// Save Public
	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		panic(err)
	}
	pubPem := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})
	saveFile(filepath.Join("keys", name+".pub"), pubPem)

	fmt.Printf("已为 '%s' 生成 RSA 密钥\n", name)
}

func generateECDSA(name string) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	// Save Private
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		panic(err)
	}
	privPem := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privBytes,
	})
	saveFile(filepath.Join("keys", name+".key"), privPem)

	// Save Public
	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		panic(err)
	}
	pubPem := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})
	saveFile(filepath.Join("keys", name+".pub"), pubPem)

	fmt.Printf("已为 '%s' 生成 ECDSA 密钥\n", name)
}

func saveFile(path string, data []byte) {
	if err := os.WriteFile(path, data, 0644); err != nil {
		panic(err)
	}
	fmt.Printf("已保存：%s\n", path)
}
