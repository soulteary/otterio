/*
 * MinIO Cloud Storage, (C) 2016-2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"errors"
	"net/http"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	jwtreq "github.com/dgrijalva/jwt-go/request"
	xjwt "github.com/soulteary/otterio/cmd/jwt"
	"github.com/soulteary/otterio/cmd/logger"
)

const (
	jwtAlgorithm = "Bearer"

	// Inter-node JWT token expiry is 15 minutes.
	defaultInterNodeJWTExpiry = 15 * time.Minute
)

var (
	errInvalidAccessKeyID = errors.New("The access key ID you provided does not exist in our records")
	errAuthentication     = errors.New("Authentication failed, check your access credentials")
	errNoAuthToken        = errors.New("JWT token missing")
)

func authenticateNode(accessKey, secretKey, audience string) (string, error) {
	claims := xjwt.NewStandardClaims()
	claims.SetExpiry(UTCNow().Add(defaultInterNodeJWTExpiry))
	claims.SetAccessKey(accessKey)
	claims.SetAudience(audience)

	jwt := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, claims)
	return jwt.SignedString([]byte(secretKey))
}

// Callback function used for parsing
func bearerTokenKey(claims *xjwt.MapClaims) ([]byte, error) {
	if claims.AccessKey == globalActiveCred.AccessKey {
		return []byte(globalActiveCred.SecretKey), nil
	}
	ok, _, err := globalIAMSys.IsTempUser(claims.AccessKey)
	if err != nil {
		if err == errNoSuchUser {
			return nil, errInvalidAccessKeyID
		}
		return nil, err
	}
	if ok {
		return []byte(globalActiveCred.SecretKey), nil
	}
	cred, ok := globalIAMSys.GetUser(claims.AccessKey)
	if !ok {
		return nil, errInvalidAccessKeyID
	}
	return []byte(cred.SecretKey), nil

}

// Check if the request is authenticated.
// Returns nil if the request is authenticated. errNoAuthToken if token missing.
// Returns errAuthentication for all other errors.
func authenticateBearerRequest(req *http.Request) (*xjwt.MapClaims, bool, error) {
	token, err := jwtreq.AuthorizationHeaderExtractor.ExtractToken(req)
	if err != nil {
		if err == jwtreq.ErrNoTokenInRequest {
			return nil, false, errNoAuthToken
		}
		return nil, false, err
	}
	claims := xjwt.NewMapClaims()
	if err := xjwt.ParseWithClaims(token, claims, bearerTokenKey); err != nil {
		return claims, false, errAuthentication
	}
	owner := claims.AccessKey == globalActiveCred.AccessKey
	return claims, owner, nil
}

func newAuthToken(audience string) string {
	cred := globalActiveCred
	token, err := authenticateNode(cred.AccessKey, cred.SecretKey, audience)
	logger.CriticalIf(GlobalContext, err)
	return token
}
