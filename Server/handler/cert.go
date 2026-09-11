package handler

import (
	"archive/zip"
	"cloudflare-tools/server/config"
	"cloudflare-tools/server/models"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type BatchApplyCertRequest struct {
	AccountID       string   `json:"accountId"`
	Domains         []string `json:"domains"`
	IncludeWildcard bool     `json:"includeWildcard"`
}

type CertResult struct {
	Domain      string   `json:"domain"`
	Success     bool     `json:"success"`
	Message     string   `json:"message"`
	Steps       []string `json:"steps"`
	CertPath    string   `json:"certPath,omitempty"`
	DownloadURL string   `json:"downloadUrl,omitempty"`
}

func BatchApplyCert(c *gin.Context) {
	var req BatchApplyCertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	if !validateBatch(c, len(req.Domains)) {
		return
	}

	acc, ok := findAccount(req.AccountID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	results := make([]CertResult, len(req.Domains))
	var wg sync.WaitGroup

	for i, domain := range req.Domains {
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg, steps, certPath := applyCertificate(acc, dom, req.IncludeWildcard)
			downloadURL := ""
			if success && certPath != "" {
				downloadURL = fmt.Sprintf("/api/certs/download/%s", filepath.Base(certPath))
			}
			results[idx] = CertResult{
				Domain:      dom,
				Success:     success,
				Message:     msg,
				Steps:       steps,
				CertPath:    certPath,
				DownloadURL: downloadURL,
			}
		}(i, domain)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func applyCertificate(acc *models.Account, domain string, includeWildcard bool) (bool, string, []string, string) {
	steps := []string{}
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if !validDomain(domain) {
		return false, "域名格式无效", []string{"✗ 域名格式无效"}, ""
	}

	acmeShPath := filepath.Join(os.Getenv("HOME"), ".acme.sh", "acme.sh")
	if _, err := os.Stat(acmeShPath); os.IsNotExist(err) {
		steps = append(steps, "错误: acme.sh 未安装")
		return false, "acme.sh not installed", steps, ""
	}
	steps = append(steps, "✓ 检查 acme.sh 环境")

	certDir := filepath.Join(config.DataPath("certs"), domain)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return false, "无法创建证书目录", append(steps, "✗ 创建证书目录失败"), ""
	}
	steps = append(steps, "✓ 创建证书目录")

	domainArgs := []string{"-d", domain}
	domainList := domain
	if includeWildcard {
		domainArgs = append(domainArgs, "-d", "*."+domain)
		domainList = domain + " + *." + domain
	}
	steps = append(steps, fmt.Sprintf("✓ 准备申请域名: %s", domainList))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, acmeShPath, append([]string{
		"--issue",
		"--dns", "dns_cf",
		"--server", "letsencrypt",
		"--accountemail", acc.Email,
	}, domainArgs...)...)

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CF_Key=%s", acc.Key),
		fmt.Sprintf("CF_Email=%s", acc.Email),
	)

	steps = append(steps, "→ 调用 acme.sh 申请证书...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(output)
		if strings.Contains(errMsg, "Domains not changed") {
			steps = append(steps, "✓ 证书已存在，准备安装")
			return installExistingCert(acmeShPath, domain, certDir, includeWildcard, steps)
		}
		steps = append(steps, "✗ 申请失败")
		steps = append(steps, fmt.Sprintf("错误详情: %s", truncateMessage(errMsg, 2000)))
		return false, "申请失败", steps, ""
	}

	steps = append(steps, "✓ 证书申请成功")
	return installExistingCert(acmeShPath, domain, certDir, includeWildcard, steps)
}

func installExistingCert(acmeShPath, domain, certDir string, includeWildcard bool, steps []string) (bool, string, []string, string) {
	domainArgs := []string{"-d", domain}
	if includeWildcard {
		domainArgs = append(domainArgs, "-d", "*."+domain)
	}

	steps = append(steps, "→ 安装证书文件...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	installCmd := exec.CommandContext(ctx, acmeShPath, append([]string{
		"--install-cert",
	}, append(domainArgs,
		"--cert-file", filepath.Join(certDir, "cert.pem"),
		"--key-file", filepath.Join(certDir, "key.pem"),
		"--fullchain-file", filepath.Join(certDir, "fullchain.pem"),
		"--ca-file", filepath.Join(certDir, "ca.pem"),
	)...)...)

	if output, err := installCmd.CombinedOutput(); err != nil {
		steps = append(steps, "✗ 证书安装失败")
		steps = append(steps, fmt.Sprintf("错误详情: %s", truncateMessage(string(output), 2000)))
		return false, "安装失败", steps, ""
	}

	steps = append(steps, "✓ 证书文件安装完成")
	steps = append(steps, "→ 打包证书为 ZIP...")

	zipPath := certDir + ".zip"
	if err := zipCertFiles(certDir, zipPath); err != nil {
		steps = append(steps, "✗ ZIP 打包失败")
		return false, "打包失败", steps, certDir
	}

	steps = append(steps, "✓ 证书打包完成")
	steps = append(steps, "✓ 全部完成，可以下载")
	return true, "申请成功", steps, zipPath
}

func zipCertFiles(sourceDir, zipPath string) error {
	zipFile, err := os.OpenFile(zipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(zipFile)

	files := []string{"cert.pem", "key.pem", "fullchain.pem", "ca.pem"}
	for _, file := range files {
		filePath := filepath.Join(sourceDir, file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue
		}

		f, err := os.Open(filePath)
		if err != nil {
			archive.Close()
			zipFile.Close()
			return err
		}

		w, err := archive.Create(file)
		if err != nil {
			f.Close()
			archive.Close()
			zipFile.Close()
			return err
		}

		if _, err := io.Copy(w, f); err != nil {
			f.Close()
			archive.Close()
			zipFile.Close()
			return err
		}
		if err := f.Close(); err != nil {
			archive.Close()
			zipFile.Close()
			return err
		}
	}
	if err := archive.Close(); err != nil {
		zipFile.Close()
		return err
	}
	return zipFile.Close()
}

func DownloadCert(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" || filepath.Base(filename) != filename || !strings.HasSuffix(filename, ".zip") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}

	filePath := filepath.Join(config.DataPath("certs"), filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/zip")
	c.File(filePath)
}

func ListCerts(c *gin.Context) {
	certsDir := config.DataPath("certs")
	if _, err := os.Stat(certsDir); os.IsNotExist(err) {
		c.JSON(http.StatusOK, []gin.H{})
		return
	}

	files, err := os.ReadDir(certsDir)
	if err != nil {
		c.JSON(http.StatusOK, []gin.H{})
		return
	}

	var certs []gin.H
	for _, file := range files {
		if info, statErr := file.Info(); statErr == nil && strings.HasSuffix(file.Name(), ".zip") {
			domain := strings.TrimSuffix(file.Name(), ".zip")
			certs = append(certs, gin.H{
				"domain":      domain,
				"filename":    file.Name(),
				"size":        info.Size(),
				"modifiedAt":  info.ModTime().Format("2006-01-02 15:04:05"),
				"downloadUrl": fmt.Sprintf("/api/certs/download/%s", file.Name()),
			})
		}
	}

	if certs == nil {
		certs = []gin.H{}
	}

	c.JSON(http.StatusOK, certs)
}

func truncateMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
