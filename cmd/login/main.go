package main

import (
	"context"
	"encoding/json"
	"flag"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
)

func main() {
	var (
		binPath string // 浏览器二进制文件路径
	)
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径")
	flag.Parse()

	// 登录时需要界面（非 headless），使用全局共享浏览器
	sb := browser.GetSharedBrowser(false, browser.WithBinPath(binPath))
	defer sb.Close()

	page := sb.NewPage()
	defer page.Close()

	action := xiaohongshu.NewLogin(page)

	// 直接进入登录流程（不检查 isLoggedIn，避免误判导致浏览器自动关闭）
	logrus.Info("开始登录流程，请扫码...")
	if err := action.Login(context.Background()); err != nil {
		logrus.Fatalf("登录失败: %v", err)
	}

	// 保存 cookies
	if err := saveCookies(page); err != nil {
		logrus.Fatalf("failed to save cookies: %v", err)
	}

	logrus.Info("登录成功，cookies 已保存！")
}

func saveCookies(page *rod.Page) error {
	cks, err := page.Browser().GetCookies()
	if err != nil {
		return err
	}

	data, err := json.Marshal(cks)
	if err != nil {
		return err
	}

	cookieLoader := cookies.NewLoadCookie(cookies.GetCookiesFilePath())
	return cookieLoader.SaveCookies(data)
}
