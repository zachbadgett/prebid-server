package adscert

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"testing"
)

func TestRequest(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	msg := `{
  "regs": {
    "ext": {}
  },
  "pcv": "a:180802:1808020000",
  "site": {
    "domain": "example.com",
    "page": "http://www.example.com/sample",
    "id": "53198e3f",
    "publisher": {
      "id": "deadb33f"
    },
    "content": {
      "language": "en",
      "url": "http://www.example.com/sample"
    },
    "cat": [
      "IAB7",
      "IAB17"
    ]
  },
  "ps": "MEYCIQD41AFz8TzYvE7uhmCACR+guf+Ih+1E6dR0CCS84vFttwIhAPJ9VALxuXtF3Vk3HOKfKL20OkY0NJVjThjWdMaUHpjE",
  "at": 2,
  "id": "94d8a111-c232-4b38-a4ac-f6aa213e4a80",
  "imp": [
    {
      "tagid": "53198e3f",
      "native": {
        "plcmtcnt": 1,
        "assets": [
          {
            "id": 11,
            "required": 0,
            "data": {
              "len": 2000,
              "type": 501
            }
          },
          {
            "id": 12,
            "required": 0,
            "data": {
              "len": 2000,
              "type": 502
            }
          },
          {
            "id": 3,
            "required": 0,
            "img": {
              "hmin": 48,
              "wmin": 48,
              "type": 2
            }
          },
          {
            "id": 1,
            "required": 1,
            "title": {
              "len": 200
            }
          },
          {
            "id": 5,
            "required": 1,
            "data": {
              "len": 140,
              "type": 1
            }
          },
          {
            "id": 6,
            "required": 1,
            "data": {
              "len": 200,
              "type": 2
            }
          },
          {
            "id": 4,
            "required": 1,
            "img": {
              "hmin": 250,
              "wmin": 300,
              "type": 3
            }
          },
          {
            "id": 2,
            "required": 0,
            "img": {
              "hmin": 48,
              "wmin": 48,
              "type": 1
            }
          }
        ],
        "request": "{\"native\":{\"plcmtcnt\":1,\"assets\":[{\"id\":11,\"required\":0,\"data\":{\"len\":2000,\"type\":501}},{\"id\":12,\"required\":0,\"data\":{\"len\":2000,\"type\":502}},{\"id\":3,\"required\":0,\"img\":{\"hmin\":48,\"wmin\":48,\"type\":2}},{\"id\":1,\"required\":1,\"title\":{\"len\":200}},{\"id\":5,\"required\":1,\"data\":{\"len\":140,\"type\":1}},{\"id\":6,\"required\":1,\"data\":{\"len\":200,\"type\":2}},{\"id\":4,\"required\":1,\"img\":{\"hmin\":250,\"wmin\":300,\"type\":3}},{\"id\":2,\"required\":0,\"img\":{\"hmin\":48,\"wmin\":48,\"type\":1}}],\"adunit\":2,\"ver\":\"1.0\",\"layout\":3}}",
        "adunit": 2,
        "ver": "1.0",
        "layout": 3
      },
      "id": "1",
      "bidfloor": 3.5,
      "secure": 0
    }
  ],
  "user": {
    "buyeruid": "16dd60de-69e3-46a2-8812-8539d4d865ad"
  },
  "device": {
    "connectiontype": 1,
    "devicetype": 2,
    "ip": "162.0.0.254",
    "model": "Unknown",
    "ua": "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/44.0.2403.130 Safari/537.36",
    "geo": {
      "country": "US",
      "metro": "803",
      "type": 2
    },
    "os": "unknown",
    "osv": "44",
    "language": "en",
    "make": "Unknown",
    "js": 1,
    "dnt": 0
  }
}
`
	br := &BidRequest{}
	json.Unmarshal([]byte(msg), br)
	privateKey, publicKey, err := LoadKeys()
	if err != nil {
		t.Error(err)
		return
	}
	msg, sig, err := CreateSignature(privateKey, br)
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("signature:\nmsg=%s\nsig=%s", msg, base64.StdEncoding.EncodeToString(sig))
	verified, err := Verify(publicKey, sha256.New(), msg, sig)
	if err != nil {
		t.Error(err)
		return
	}
	if !verified {
		t.Error("signature not verified")
	}
}
