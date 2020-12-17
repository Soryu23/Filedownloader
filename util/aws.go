package util

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

func getSign(r *http.Request) string {
	//https://docs.aws.amazon.com/zh_cn/AmazonS3/latest/API/sigv4-query-string-auth.html
	var (
		canonicalRequest string
		stringToSign     string
		signingKey       []byte
		sign             string
		accessKeyID      string
		awsRegion        string
		timeString       string
	)

	router := r.URL.String()
	regResource, _ := regexp.Compile(`\/[a-zA-Z0-9.-]+\?`)
	resource := regResource.FindString(router)
	regParameter, _ := regexp.Compile(`\?\S+`)
	parameter := regParameter.FindString(router)
	parameters := strings.Split(parameter[1:], "&")
	sort.Strings(parameters)
	parameter = ""
	for _, p := range parameters {
		if strings.HasPrefix(p, "X-Amz-Credential") {
			accessKeyID = p[17:]
			tmp := strings.Split(accessKeyID, `%2F`)
			awsRegion = tmp[2]
			accessKeyID = tmp[0]
		}
		if !strings.HasPrefix(p, "X-Amz-Signature") {
			parameter += p + "&"
		}
		if strings.HasPrefix(p, "X-Amz-Date") {
			timeString = p[11:]
		}
	}

	canonicalHeader := "host:" + strings.Trim(r.URL.Host, " ") + "\n"
	signedHeader := "host\n"

	// for i := range r.Header {
	// headname := strings.ToLower(i)
	// headvalue := ""
	// for _, j := range r.Header[i] {
	// headvalue += strings.Trim(j, " ")
	// }
	// headvalue = strings.ToLower(headvalue)
	// canonicalHeader += headname + ":" + headvalue + "\n"
	// signedHeader += headvalue + ";"
	// }

	//signedHeader = signedHeader[:len(signedHeader)-1]

	httpVerb := r.Method + "\n"
	canonicalURI := resource[:len(resource)-1] + "\n"
	canonicalQueryString := parameter[:len(parameter)-1] + "\n"

	canonicalRequest = httpVerb + canonicalURI + canonicalQueryString + canonicalHeader + signedHeader + "UNSIGNED-PAYLOAD"

	//timeString := time.Now().Format("20060102T150405Z")
	yymmdd := timeString[:8]
	scope := yymmdd + "/" + awsRegion + "/" + "s3/aws4_request"
	stringToSign = "AWS4-HMAC-SHA256\n" + timeString + "\n" + scope + "\n" + SHA256(canonicalRequest)

	signingKey = HMAC_SHA256(HMAC_SHA256(HMAC_SHA256(HMAC_SHA256([]byte("AWS4"+"AwsSecretAccessKey"), yymmdd), awsRegion), "s3"), "aws4_request")

	sign = hex.EncodeToString(HMAC_SHA256(signingKey, stringToSign))
	return sign
}

func SHA256(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func HMAC_SHA256(a []byte, b string) []byte {
	sha256 := sha256.New
	hash := hmac.New(sha256, a)
	hash.Write([]byte(b))
	return hash.Sum(nil)
}
