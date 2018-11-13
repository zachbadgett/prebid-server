package adcert

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"hash"
	"math/big"
	"sort"
)

func CreateSignature(privateKey *ecdsa.PrivateKey, request *BidRequest) (msg string, sig []byte, err error) {
	var bundle string
	var domain string
	var ip, ipv6, ifa, ua string
	if a := request.App; a != nil {
		bundle = a.Bundle
	}
	if s := request.Site; s != nil {
		domain = s.Domain
	}
	if d := request.Device; d != nil {
		ip = d.IP
		ipv6 = d.IPv6
		ifa = d.IFA
		ua = d.UA
	}
	m := map[string]string{
		"tid":     request.ID,
		"cert":    fmt.Sprintf("ads-cert.%s.txt", request.PublisherCertificateVersion),
		"domain":  domain,
		"bundle":  bundle,
		"consent": "",
		"ft":      "d",
		"ip":      ip,
		"ipv6":    ipv6,
		"ifa":     ifa,
		"ua":      ua,
		"w":       "",
		"h":       "",
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	msg = keys[0] + "=" + m[keys[0]]
	for _, k := range keys[1:] {
		msg += "&"
		msg += k + "=" + m[k]
	}
	sig, err = Sign(privateKey, sha256.New(), msg)
	if err != nil {
		return msg, nil, err
	}
	//fmt.Printf("publisher signature: %s\n", base64.StdEncoding.EncodeToString(sig))
	return msg, sig, nil
}

func LoadKeys() (privateKey *ecdsa.PrivateKey, publicKey *ecdsa.PublicKey, err error) {
	cert, err := tls.X509KeyPair([]byte(public), []byte(private))
	if err != nil {
		return nil, nil, err
	}
	privateKey = cert.PrivateKey.(*ecdsa.PrivateKey)
	cert2, _ := x509.ParseCertificate(cert.Certificate[0])
	publicKey = cert2.PublicKey.(*ecdsa.PublicKey)
	return privateKey, publicKey, err
}

//func LoadKeys(pub, priv string) (publicKey *ecdsa.PublicKey, privateKey *ecdsa.PrivateKey, err error) {
//	pv, err := ioutil.ReadFile(priv)
//	if err != nil {
//		return nil, nil, err
//	}
//	pb, err := ioutil.ReadFile(pub)
//	if err != nil {
//		return nil, nil, err
//	}
//	cert, err := tls.X509KeyPair(pb, pv)
//	if err != nil {
//		// todo: fix
//		log.Fatal(err)
//	}
//	privateKey = cert.PrivateKey.(*ecdsa.PrivateKey)
//	cert2, _ := x509.ParseCertificate(cert.Certificate[0])
//	publicKey = cert2.PublicKey.(*ecdsa.PublicKey)
//	return publicKey, privateKey, err
//}

func Sign(privateKey *ecdsa.PrivateKey, digest hash.Hash, msg string) ([]byte, error) {
	if _, err := digest.Write([]byte(msg)); err != nil {
		return nil, err
	}
	out, err := privateKey.Sign(rand.Reader, digest.Sum(nil), nil)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func Verify(publicKey *ecdsa.PublicKey, digest hash.Hash, msg string, signature []byte) (bool, error) {
	if _, err := digest.Write([]byte(msg)); err != nil {
		return false, err
	}
	ecdsaSig := new(struct{ R, S *big.Int })
	if _, err := asn1.Unmarshal(signature, ecdsaSig); err != nil {
		return false, err
	}
	if !ecdsa.Verify(publicKey, digest.Sum(nil), ecdsaSig.R, ecdsaSig.S) {
		return false, fmt.Errorf("failed ECDSA signature validation")
	}
	return true, nil
}
