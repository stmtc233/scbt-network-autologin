package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"
)

var dateFormats = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02",
}

func parseExpireTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range dateFormats {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func LoadAccountsFromCSV(filePath string, primaryAccountUsername string) ([]*Account, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取CSV文件 %s 失败: %w", filePath, err)
	}

	// 去除 UTF-8 BOM
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	reader := csv.NewReader(bytes.NewReader(data))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // 允许变长列

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析CSV内容失败: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV文件为空")
	}

	colUser := 0
	colPass := 1
	colPkg := -1
	colExp := -1

	startRow := 0
	firstRow := records[0]
	hasHeader := false

	for i, col := range firstRow {
		cleanCol := strings.TrimSpace(strings.ToLower(col))
		if strings.Contains(cleanCol, "学号") || strings.Contains(cleanCol, "账号") || strings.Contains(cleanCol, "user") {
			colUser = i
			hasHeader = true
		} else if strings.Contains(cleanCol, "密码") || strings.Contains(cleanCol, "pass") {
			colPass = i
			hasHeader = true
		} else if strings.Contains(cleanCol, "套餐") || strings.Contains(cleanCol, "pkg") || strings.Contains(cleanCol, "plan") {
			colPkg = i
			hasHeader = true
		} else if strings.Contains(cleanCol, "过期") || strings.Contains(cleanCol, "expire") || strings.Contains(cleanCol, "时间") {
			colExp = i
			hasHeader = true
		}
	}

	if hasHeader {
		startRow = 1
	}

	var accounts []*Account
	now := time.Now()

	for r := startRow; r < len(records); r++ {
		row := records[r]
		if len(row) <= colUser || len(row) <= colPass {
			continue
		}
		user := strings.TrimSpace(row[colUser])
		pass := strings.TrimSpace(row[colPass])
		if user == "" || pass == "" {
			continue
		}

		pkg := ""
		if colPkg >= 0 && len(row) > colPkg {
			pkg = strings.TrimSpace(row[colPkg])
		}

		expStr := ""
		var expTime time.Time
		isExpired := false
		if colExp >= 0 && len(row) > colExp {
			expStr = strings.TrimSpace(row[colExp])
			if t, ok := parseExpireTime(expStr); ok {
				expTime = t
				if expTime.Before(now) {
					isExpired = true
				}
			}
		}

		acc := &Account{
			Username:   user,
			Password:   pass,
			Package:    pkg,
			ExpireStr:  expStr,
			ExpireTime: expTime,
			Status:     AccountStatusIdle,
		}

		if isExpired {
			acc.Status = AccountStatusExpired
			acc.FailReason = fmt.Sprintf("账号已于 %s 过期", expStr)
		}

		if user == primaryAccountUsername {
			acc.IsPrimary = true
		}

		accounts = append(accounts, acc)
	}

	if len(accounts) == 0 {
		return nil, fmt.Errorf("CSV文件中未找到有效账号数据")
	}

	// 如果未指定主账号，默认将第一个未过期的账号指定为主账号
	hasPrimary := false
	for _, acc := range accounts {
		if acc.IsPrimary {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		for _, acc := range accounts {
			if acc.Status != AccountStatusExpired {
				acc.IsPrimary = true
				break
			}
		}
	}

	return accounts, nil
}
