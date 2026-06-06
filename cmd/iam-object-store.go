/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	jwtgo "github.com/dgrijalva/jwt-go"

	"github.com/soulteary/otterio/cmd/config/identity/ldap"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

// envIAMLDAPDNMigration is the feature flag that gates the one-time
// LDAP DN normalization migration in loadMappedPolicies. Defaults to "on".
// Operators may set OTTERIO_IAM_LDAP_DN_MIGRATION=off to defer the migration
// (e.g. for an audit dry-run) - while the flag is off the in-memory policy
// map will be sharded across DN case variants exactly as on disk.
const envIAMLDAPDNMigration = "OTTERIO_IAM_LDAP_DN_MIGRATION"

// migrateMappedPolicyToCanonical re-keys a single on-disk policy mapping
// whose name is an LDAP DN to its NormalizeDN canonical form.
//
// It is invoked at boot from loadMappedPolicies for the regular-user and
// group code paths when the IAM layer is configured with
// LDAPUsersSysType. Three cases:
//
//  1. The on-disk name is already canonical: no-op.
//  2. The on-disk name is non-canonical and the canonical key is FREE:
//     the caller has already loaded the mapping into m[name]; we move it
//     to m[canonical], remove the old in-memory entry, copy the on-disk
//     blob to the canonical path and delete the old path.
//  3. The on-disk name is non-canonical and the canonical key is OCCUPIED
//     by a mapping with different content: we keep the lex-min name as
//     winner (deterministic) and move the loser to a `.conflict-<ns>`
//     side-car so an operator can reconcile it manually. Both the load
//     and the side-car moves are best-effort: an error is logged but we
//     do not abort the whole IAM load - the running policy map is still
//     correct, and the operator can retry the migration after fixing
//     the storage layer.
//
// On parse-failure of the on-disk DN we log and skip; the caller has
// already populated the in-memory map with the literal name as a fallback,
// matching pre-normalization behavior for that one entry.
func (iamOS *IAMObjectStore) migrateMappedPolicyToCanonical(ctx context.Context,
	rawName string, userType IAMUserType, isGroup bool, m map[string]MappedPolicy) {

	if rawName == "" {
		return
	}
	canonical, err := ldap.NormalizeDN(rawName)
	if err != nil {
		logger.LogIf(ctx, fmt.Errorf("LDAP DN migration: skipping unparseable on-disk name %q: %w", rawName, err))
		return
	}
	if canonical == rawName {
		return
	}

	rawPath := getMappedPolicyPath(rawName, userType, isGroup)
	canonicalPath := getMappedPolicyPath(canonical, userType, isGroup)

	rawPolicy, hasRaw := m[rawName]
	canonicalPolicy, hasCanonical := m[canonical]

	var winner MappedPolicy
	var winnerName string
	loserName := ""
	switch {
	case hasCanonical && hasRaw && reflect.DeepEqual(rawPolicy, canonicalPolicy):
		// duplicate content: just delete the raw side, keep the canonical.
		winner = canonicalPolicy
		winnerName = canonical
		loserName = rawName
	case hasCanonical && hasRaw && !reflect.DeepEqual(rawPolicy, canonicalPolicy):
		// real conflict: pick lex-min as winner, side-car the other.
		if rawName < canonical {
			winner = rawPolicy
			winnerName = canonical // we still want the in-memory key canonical
			loserName = ""         // canonical itself is the loser content; it goes to .conflict
			logger.LogIf(ctx, fmt.Errorf("LDAP DN migration conflict: %q (winner) and %q (loser): keeping winner content; loser archived to %s.conflict-%d",
				rawName, canonical, canonicalPath, time.Now().UnixNano()))
			iamOS.archiveConflict(ctx, canonicalPath, time.Now())
		} else {
			winner = canonicalPolicy
			winnerName = canonical
			logger.LogIf(ctx, fmt.Errorf("LDAP DN migration conflict: %q (loser) and %q (winner): keeping winner content; loser archived to %s.conflict-%d",
				rawName, canonical, rawPath, time.Now().UnixNano()))
			iamOS.archiveConflict(ctx, rawPath, time.Now())
		}
	case hasRaw && !hasCanonical:
		winner = rawPolicy
		winnerName = canonical
		loserName = rawName
	default:
		// nothing to do
		return
	}

	// In-memory: ensure the canonical key holds the winner and the raw key
	// is gone.
	m[canonical] = winner
	if winnerName != rawName {
		delete(m, rawName)
	}
	_ = loserName // currently used only for clarity; future audit log hook.

	// On-disk: copy the winner blob to the canonical path and delete the
	// raw path. Migration is gated by OTTERIO_IAM_LDAP_DN_MIGRATION; when
	// disabled we leave disk untouched so an operator can dry-run.
	if !iamLDAPDNMigrationEnabled() {
		return
	}
	if err := iamOS.saveMappedPolicy(ctx, canonical, userType, isGroup, winner); err != nil {
		logger.LogIf(ctx, fmt.Errorf("LDAP DN migration: failed to write canonical mapping %q: %w", canonicalPath, err))
		return
	}
	if rawPath != canonicalPath {
		if err := iamOS.deleteIAMConfig(ctx, rawPath); err != nil && err != errConfigNotFound {
			logger.LogIf(ctx, fmt.Errorf("LDAP DN migration: failed to remove old non-canonical mapping %q: %w", rawPath, err))
		}
	}
}

// archiveConflict best-effort renames the IAM mapping at p to a
// `<p>.conflict-<unix-nano>` sentinel so a human can reconcile it later.
// We deliberately do not surface failures: a failed archive must not block
// the IAM load, and the caller has already logged the conflict.
func (iamOS *IAMObjectStore) archiveConflict(ctx context.Context, p string, when time.Time) {
	if !iamLDAPDNMigrationEnabled() {
		return
	}
	// "Read original bytes, write to .conflict-<ts>, delete original".
	raw, err := readConfig(ctx, iamOS.objAPI, p)
	if err != nil {
		// Nothing to archive (already gone or unreadable); fine.
		return
	}
	conflictPath := fmt.Sprintf("%s.conflict-%d", p, when.UnixNano())
	if err := saveConfig(ctx, iamOS.objAPI, conflictPath, raw); err != nil {
		logger.LogIf(ctx, fmt.Errorf("LDAP DN migration: failed to write conflict archive %q: %w", conflictPath, err))
		return
	}
	if err := iamOS.deleteIAMConfig(ctx, p); err != nil && err != errConfigNotFound {
		logger.LogIf(ctx, fmt.Errorf("LDAP DN migration: failed to remove conflicting mapping %q after archiving: %w", p, err))
	}
}

func iamLDAPDNMigrationEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envIAMLDAPDNMigration)))
	switch v {
	case "", "on", "true", "1", "yes":
		return true
	default:
		return false
	}
}

// IAMObjectStore implements IAMStorageAPI
type IAMObjectStore struct {
	// Protect assignment to objAPI
	sync.RWMutex

	objAPI ObjectLayer
}

func newIAMObjectStore(objAPI ObjectLayer) *IAMObjectStore {
	return &IAMObjectStore{objAPI: objAPI}
}

func (iamOS *IAMObjectStore) lock() {
	iamOS.Lock()
}

func (iamOS *IAMObjectStore) unlock() {
	iamOS.Unlock()
}

func (iamOS *IAMObjectStore) rlock() {
	iamOS.RLock()
}

func (iamOS *IAMObjectStore) runlock() {
	iamOS.RUnlock()
}

// Migrate users directory in a single scan.
//
// 1. Migrate user policy from:
//
// `iamConfigUsersPrefix + "<username>/policy.json"`
//
// to:
//
// `iamConfigPolicyDBUsersPrefix + "<username>.json"`.
//
// 2. Add versioning to the policy json file in the new
// location.
//
// 3. Migrate user identity json file to include version info.
func (iamOS *IAMObjectStore) migrateUsersConfigToV1(ctx context.Context, isSTS bool) error {
	basePrefix := iamConfigUsersPrefix
	if isSTS {
		basePrefix = iamConfigSTSPrefix
	}

	for item := range listIAMConfigItems(ctx, iamOS.objAPI, basePrefix) {
		if item.Err != nil {
			return item.Err
		}

		user := path.Dir(item.Item)
		{
			// 1. check if there is policy file in old location.
			oldPolicyPath := pathJoin(basePrefix, user, iamPolicyFile)
			var policyName string
			if err := iamOS.loadIAMConfig(ctx, &policyName, oldPolicyPath); err != nil {
				switch err {
				case errConfigNotFound:
					// This case means it is already
					// migrated or there is no policy on
					// user.
				default:
					// File may be corrupt or network error
				}

				// Nothing to do on the policy file,
				// so move on to check the id file.
				goto next
			}

			// 2. copy policy file to new location.
			mp := newMappedPolicy(policyName)
			userType := regularUser
			if isSTS {
				userType = stsUser
			}
			if err := iamOS.saveMappedPolicy(ctx, user, userType, false, mp); err != nil {
				return err
			}

			// 3. delete policy file from old
			// location. Ignore error.
			iamOS.deleteIAMConfig(ctx, oldPolicyPath)
		}
	next:
		// 4. check if user identity has old format.
		identityPath := pathJoin(basePrefix, user, iamIdentityFile)
		var cred auth.Credentials
		if err := iamOS.loadIAMConfig(ctx, &cred, identityPath); err != nil {
			switch err {
			case errConfigNotFound:
				// This should not happen.
			default:
				// File may be corrupt or network error
			}
			continue
		}

		// If the file is already in the new format,
		// then the parsed auth.Credentials will have
		// the zero value for the struct.
		var zeroCred auth.Credentials
		if cred.Equal(zeroCred) {
			// nothing to do
			continue
		}

		// Found a id file in old format. Copy value
		// into new format and save it.
		cred.AccessKey = user
		u := newUserIdentity(cred)
		if err := iamOS.saveIAMConfig(ctx, u, identityPath); err != nil {
			logger.LogIf(ctx, err)
			return err
		}

		// Nothing to delete as identity file location
		// has not changed.
	}
	return nil

}

func (iamOS *IAMObjectStore) migrateToV1(ctx context.Context) error {
	var iamFmt iamFormat
	path := getIAMFormatFilePath()
	if err := iamOS.loadIAMConfig(ctx, &iamFmt, path); err != nil {
		switch err {
		case errConfigNotFound:
			// Need to migrate to V1.
		default:
			return err
		}
	} else {
		if iamFmt.Version >= iamFormatVersion1 {
			// Nothing to do.
			return nil
		}
		// This case should not happen
		// (i.e. Version is 0 or negative.)
		return errors.New("got an invalid IAM format version")
	}

	// Migrate long-term users
	if err := iamOS.migrateUsersConfigToV1(ctx, false); err != nil {
		logger.LogIf(ctx, err)
		return err
	}
	// Migrate STS users
	if err := iamOS.migrateUsersConfigToV1(ctx, true); err != nil {
		logger.LogIf(ctx, err)
		return err
	}
	// Save iam format to version 1.
	if err := iamOS.saveIAMConfig(ctx, newIAMFormatVersion1(), path); err != nil {
		logger.LogIf(ctx, err)
		return err
	}
	return nil
}

// Should be called under config migration lock
func (iamOS *IAMObjectStore) migrateBackendFormat(ctx context.Context) error {
	return iamOS.migrateToV1(ctx)
}

func (iamOS *IAMObjectStore) saveIAMConfig(ctx context.Context, item interface{}, path string, _ ...options) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if globalConfigEncrypted {
		data, err = madmin.EncryptData(globalActiveCred.String(), data)
		if err != nil {
			return err
		}
	}
	return saveConfig(ctx, iamOS.objAPI, path, data)
}

func (iamOS *IAMObjectStore) loadIAMConfig(ctx context.Context, item interface{}, path string) error {
	data, err := readConfig(ctx, iamOS.objAPI, path)
	if err != nil {
		return err
	}
	if globalConfigEncrypted && !utf8.Valid(data) {
		data, err = madmin.DecryptData(globalActiveCred.String(), bytes.NewReader(data))
		if err != nil {
			return err
		}
	}
	return json.Unmarshal(data, item)
}

func (iamOS *IAMObjectStore) deleteIAMConfig(ctx context.Context, path string) error {
	return deleteConfig(ctx, iamOS.objAPI, path)
}

func (iamOS *IAMObjectStore) loadPolicyDoc(ctx context.Context, policy string, m map[string]iampolicy.Policy) error {
	var p iampolicy.Policy
	err := iamOS.loadIAMConfig(ctx, &p, getPolicyDocPath(policy))
	if err != nil {
		if err == errConfigNotFound {
			return errNoSuchPolicy
		}
		return err
	}
	m[policy] = p
	return nil
}

func (iamOS *IAMObjectStore) loadPolicyDocs(ctx context.Context, m map[string]iampolicy.Policy) error {
	for item := range listIAMConfigItems(ctx, iamOS.objAPI, iamConfigPoliciesPrefix) {
		if item.Err != nil {
			return item.Err
		}

		policyName := path.Dir(item.Item)
		if err := iamOS.loadPolicyDoc(ctx, policyName, m); err != nil && err != errNoSuchPolicy {
			return err
		}
	}
	return nil
}

func (iamOS *IAMObjectStore) loadUser(ctx context.Context, user string, userType IAMUserType, m map[string]auth.Credentials) error {
	var u UserIdentity
	err := iamOS.loadIAMConfig(ctx, &u, getUserIdentityPath(user, userType))
	if err != nil {
		if err == errConfigNotFound {
			return errNoSuchUser
		}
		return err
	}

	if u.Credentials.IsExpired() {
		// Delete expired identity - ignoring errors here.
		iamOS.deleteIAMConfig(ctx, getUserIdentityPath(user, userType))
		iamOS.deleteIAMConfig(ctx, getMappedPolicyPath(user, userType, false))
		return nil
	}

	// If this is a service account, rotate the session key if needed
	if globalOldCred.IsValid() && u.Credentials.IsServiceAccount() {
		if !globalOldCred.Equal(globalActiveCred) {
			m := jwtgo.MapClaims{}
			stsTokenCallback := func(_ *jwtgo.Token) (interface{}, error) {
				return []byte(globalOldCred.SecretKey), nil
			}
			if _, err := jwtgo.ParseWithClaims(u.Credentials.SessionToken, m, stsTokenCallback); err == nil {
				jwt := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, jwtgo.MapClaims(m))
				if token, err := jwt.SignedString([]byte(globalActiveCred.SecretKey)); err == nil {
					u.Credentials.SessionToken = token
					err := iamOS.saveIAMConfig(ctx, &u, getUserIdentityPath(user, userType))
					if err != nil {
						return err
					}
				}
			}
		}
	}

	if u.Credentials.AccessKey == "" {
		u.Credentials.AccessKey = user
	}

	m[user] = u.Credentials
	return nil
}

func (iamOS *IAMObjectStore) loadUsers(ctx context.Context, userType IAMUserType, m map[string]auth.Credentials) error {
	var basePrefix string
	switch userType {
	case srvAccUser:
		basePrefix = iamConfigServiceAccountsPrefix
	case stsUser:
		basePrefix = iamConfigSTSPrefix
	default:
		basePrefix = iamConfigUsersPrefix
	}

	for item := range listIAMConfigItems(ctx, iamOS.objAPI, basePrefix) {
		if item.Err != nil {
			return item.Err
		}

		userName := path.Dir(item.Item)
		if err := iamOS.loadUser(ctx, userName, userType, m); err != nil && err != errNoSuchUser {
			return err
		}
	}
	return nil
}

func (iamOS *IAMObjectStore) loadGroup(ctx context.Context, group string, m map[string]GroupInfo) error {
	var g GroupInfo
	err := iamOS.loadIAMConfig(ctx, &g, getGroupInfoPath(group))
	if err != nil {
		if err == errConfigNotFound {
			return errNoSuchGroup
		}
		return err
	}
	m[group] = g
	return nil
}

func (iamOS *IAMObjectStore) loadGroups(ctx context.Context, m map[string]GroupInfo) error {
	for item := range listIAMConfigItems(ctx, iamOS.objAPI, iamConfigGroupsPrefix) {
		if item.Err != nil {
			return item.Err
		}

		group := path.Dir(item.Item)
		if err := iamOS.loadGroup(ctx, group, m); err != nil && err != errNoSuchGroup {
			return err
		}
	}
	return nil
}

func (iamOS *IAMObjectStore) loadMappedPolicy(ctx context.Context, name string, userType IAMUserType, isGroup bool,
	m map[string]MappedPolicy) error {

	var p MappedPolicy
	err := iamOS.loadIAMConfig(ctx, &p, getMappedPolicyPath(name, userType, isGroup))
	if err != nil {
		if err == errConfigNotFound {
			return errNoSuchPolicy
		}
		return err
	}
	m[name] = p
	return nil
}

func (iamOS *IAMObjectStore) loadMappedPolicies(ctx context.Context, userType IAMUserType, isGroup bool, m map[string]MappedPolicy) error {
	var basePath string
	if isGroup {
		basePath = iamConfigPolicyDBGroupsPrefix
	} else {
		switch userType {
		case srvAccUser:
			basePath = iamConfigPolicyDBServiceAccountsPrefix
		case stsUser:
			basePath = iamConfigPolicyDBSTSUsersPrefix
		default:
			basePath = iamConfigPolicyDBUsersPrefix
		}
	}
	// rawNames keeps the on-disk keys in their original (possibly
	// non-canonical) form so the post-load LDAP DN migration can rewrite
	// them. Skipping conflict-side-cars is intentional: those files are
	// quarantined for human review, not loaded into the running policy map.
	var rawNames []string
	for item := range listIAMConfigItems(ctx, iamOS.objAPI, basePath) {
		if item.Err != nil {
			return item.Err
		}

		policyFile := item.Item
		if strings.Contains(policyFile, ".conflict-") {
			continue
		}
		userOrGroupName := strings.TrimSuffix(policyFile, ".json")
		if err := iamOS.loadMappedPolicy(ctx, userOrGroupName, userType, isGroup, m); err != nil && err != errNoSuchPolicy {
			return err
		}
		rawNames = append(rawNames, userOrGroupName)
	}

	// SECURITY (LDAP DN normalization): when the IAM layer is in LDAP mode
	// and the names we just loaded are LDAP DNs (regular user / group, but
	// not service-account or STS), re-key them to their canonical NormalizeDN
	// form. See migrateMappedPolicyToCanonical for the conflict policy.
	// This runs at most once per startup; once on-disk paths are canonical
	// the inner loop is a no-op.
	if globalIAMSys != nil && globalIAMSys.usersSysType == LDAPUsersSysType &&
		(isGroup || userType == regularUser) {
		for _, raw := range rawNames {
			iamOS.migrateMappedPolicyToCanonical(ctx, raw, userType, isGroup, m)
		}
	}
	return nil
}

// Refresh IAMSys. If an object layer is passed in use that, otherwise load from global.
func (iamOS *IAMObjectStore) loadAll(ctx context.Context, sys *IAMSys) error {
	return sys.Load(ctx, iamOS)
}

func (iamOS *IAMObjectStore) savePolicyDoc(ctx context.Context, policyName string, p iampolicy.Policy) error {
	return iamOS.saveIAMConfig(ctx, &p, getPolicyDocPath(policyName))
}

func (iamOS *IAMObjectStore) saveMappedPolicy(ctx context.Context, name string, userType IAMUserType, isGroup bool, mp MappedPolicy, opts ...options) error {
	return iamOS.saveIAMConfig(ctx, mp, getMappedPolicyPath(name, userType, isGroup), opts...)
}

func (iamOS *IAMObjectStore) saveUserIdentity(ctx context.Context, name string, userType IAMUserType, u UserIdentity, opts ...options) error {
	return iamOS.saveIAMConfig(ctx, u, getUserIdentityPath(name, userType), opts...)
}

func (iamOS *IAMObjectStore) saveGroupInfo(ctx context.Context, name string, gi GroupInfo) error {
	return iamOS.saveIAMConfig(ctx, gi, getGroupInfoPath(name))
}

func (iamOS *IAMObjectStore) deletePolicyDoc(ctx context.Context, name string) error {
	err := iamOS.deleteIAMConfig(ctx, getPolicyDocPath(name))
	if err == errConfigNotFound {
		err = errNoSuchPolicy
	}
	return err
}

func (iamOS *IAMObjectStore) deleteMappedPolicy(ctx context.Context, name string, userType IAMUserType, isGroup bool) error {
	err := iamOS.deleteIAMConfig(ctx, getMappedPolicyPath(name, userType, isGroup))
	if err == errConfigNotFound {
		err = errNoSuchPolicy
	}
	return err
}

func (iamOS *IAMObjectStore) deleteUserIdentity(ctx context.Context, name string, userType IAMUserType) error {
	err := iamOS.deleteIAMConfig(ctx, getUserIdentityPath(name, userType))
	if err == errConfigNotFound {
		err = errNoSuchUser
	}
	return err
}

func (iamOS *IAMObjectStore) deleteGroupInfo(ctx context.Context, name string) error {
	err := iamOS.deleteIAMConfig(ctx, getGroupInfoPath(name))
	if err == errConfigNotFound {
		err = errNoSuchGroup
	}
	return err
}

// helper type for listIAMConfigItems
type itemOrErr struct {
	Item string
	Err  error
}

// Lists files or dirs in the otterioMetaBucket at the given path
// prefix. If dirs is true, only directories are listed, otherwise
// only objects are listed. All returned items have the pathPrefix
// removed from their names.
func listIAMConfigItems(ctx context.Context, objAPI ObjectLayer, pathPrefix string) <-chan itemOrErr {
	ch := make(chan itemOrErr)

	go func() {
		defer close(ch)

		// Allocate new results channel to receive ObjectInfo.
		objInfoCh := make(chan ObjectInfo)

		if err := objAPI.Walk(ctx, otterioMetaBucket, pathPrefix, objInfoCh, ObjectOptions{}); err != nil {
			select {
			case ch <- itemOrErr{Err: err}:
			case <-ctx.Done():
			}
			return
		}

		for obj := range objInfoCh {
			item := strings.TrimPrefix(obj.Name, pathPrefix)
			item = strings.TrimSuffix(item, SlashSeparator)
			select {
			case ch <- itemOrErr{Item: item}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

func (iamOS *IAMObjectStore) watch(ctx context.Context, sys *IAMSys) {
	// Refresh IAMSys.
	for {
		time.Sleep(globalRefreshIAMInterval)
		if err := iamOS.loadAll(ctx, sys); err != nil {
			logger.LogIf(ctx, err)
		}
	}
}
