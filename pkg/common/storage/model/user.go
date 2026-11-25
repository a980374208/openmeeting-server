// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"strings"
	"unicode"
)

const (
	UserStatusActive   int32 = 0
	UserStatusDisabled int32 = 1
)

type User struct {
	UserID         string   `bson:"user_id"`
	Account        string   `bson:"account"`
	Nickname       string   `bson:"nickname"`
	Password       string   `bson:"password"`
	SaltValue      string   `bson:"salt_value"`
	EmployeeNo     string   `bson:"employee_no,omitempty"`
	Email          string   `bson:"email,omitempty"`
	Phone          string   `bson:"phone,omitempty"`
	JobTitle       string   `bson:"job_title,omitempty"`
	Signature      string   `bson:"signature,omitempty"`
	AvatarURL      string   `bson:"avatar_url,omitempty"`
	PrimaryDeptIDs []string `bson:"primary_dept_ids,omitempty"`
	SearchTokens   []string `bson:"search_tokens,omitempty"`
	Status         int32    `bson:"status"`
	UpdatedAt      int64    `bson:"updated_at,omitempty"`
}

// BuildSearchTokens generates normalized lowercase search tokens for safe prefix
// matching without unescaped regex scans.
func BuildSearchTokens(u *User, extraTerms ...string) []string {
	if u == nil {
		return nil
	}
	seen := make(map[string]struct{}, 16)
	var out []string
	addToken := func(raw string) {
		trimmed := strings.ToLower(strings.TrimSpace(raw))
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; !ok {
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
		// Split by whitespace / separators to allow word-level prefix queries.
		parts := strings.FieldsFunc(trimmed, func(r rune) bool {
			return unicode.IsSpace(r) || r == '/' || r == '-' || r == '_' || r == '@' || r == '.'
		})
		for _, p := range parts {
			if p == "" {
				continue
			}
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				out = append(out, p)
			}
		}
		// Also emit rune-suffixes for short CJK names so searching given name works.
		runes := []rune(trimmed)
		if len(runes) >= 2 && len(runes) <= 8 {
			hasCJK := false
			for _, r := range runes {
				if unicode.Is(unicode.Han, r) {
					hasCJK = true
					break
				}
			}
			if hasCJK {
				for i := 1; i < len(runes); i++ {
					sub := string(runes[i:])
					if _, ok := seen[sub]; !ok {
						seen[sub] = struct{}{}
						out = append(out, sub)
					}
				}
			}
		}
	}

	addToken(u.UserID)
	addToken(u.Account)
	addToken(u.Nickname)
	addToken(u.EmployeeNo)
	addToken(u.Email)
	addToken(u.JobTitle)
	for _, term := range extraTerms {
		addToken(term)
	}
	return out
}
