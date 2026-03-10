package browser

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

type browserConfig struct {
	binPath string
}

type Option func(*browserConfig)

func WithBinPath(binPath string) Option {
	return func(c *browserConfig) {
		c.binPath = binPath
	}
}

// maskProxyCredentials masks username and password in proxy URL for safe logging.
func maskProxyCredentials(proxyURL string) string {
	u, err := url.Parse(proxyURL)
	if err != nil || u.User == nil {
		return proxyURL
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword("***", "***")
	} else {
		u.User = url.User("***")
	}
	return u.String()
}

// SharedBrowser 是一个全局共享的浏览器单例。
// 登录和发布使用同一个 Chrome 进程，这样登录后的 session（cookies + localStorage）
// 在发布时自动可用，无需任何 cookie 文件导入。
// 服务重启时，自动从 cookies.json 加载上次保存的 cookies。
type SharedBrowser struct {
	browser  *rod.Browser
	launcher *launcher.Launcher
	mu       sync.Mutex
}

var (
	globalBrowser *SharedBrowser
	globalMu      sync.Mutex
)

// GetSharedBrowser 返回全局共享的浏览器实例（单例模式）
// 首次调用时创建 Chrome 进程并加载已保存的 cookies，后续调用复用同一个进程
func GetSharedBrowser(headless bool, options ...Option) *SharedBrowser {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalBrowser != nil {
		return globalBrowser
	}

	cfg := &browserConfig{}
	for _, opt := range options {
		opt(cfg)
	}

	l := launcher.New().
		Headless(headless).
		Set("--no-sandbox")

	if cfg.binPath != "" {
		l = l.Bin(cfg.binPath)
	}

	if proxy := os.Getenv("XHS_PROXY"); proxy != "" {
		l = l.Proxy(proxy)
		logrus.Infof("Using proxy: %s", maskProxyCredentials(proxy))
	}

	u := l.MustLaunch()
	logrus.Info("[browser] 全局共享浏览器已启动")

	b := rod.New().
		ControlURL(u).
		MustConnect()

	globalBrowser = &SharedBrowser{
		browser:  b,
		launcher: l,
	}

	// 自动加载已保存的 cookies（服务重启后免扫码）
	globalBrowser.loadSavedCookies()

	return globalBrowser
}

// loadSavedCookies 从 cookies.json 加载并注入到浏览器
func (sb *SharedBrowser) loadSavedCookies() {
	cookiePath := cookies.GetCookiesFilePath()
	loader := cookies.NewLoadCookie(cookiePath)
	data, err := loader.LoadCookies()
	if err != nil {
		logrus.Debugf("[browser] 未找到已保存的 cookies（%s），需要扫码登录", cookiePath)
		return
	}

	var cks []*proto.NetworkCookie
	if err := json.Unmarshal(data, &cks); err != nil {
		logrus.Warnf("[browser] cookies.json 解析失败: %v", err)
		return
	}

	if len(cks) == 0 {
		logrus.Debug("[browser] cookies.json 为空")
		return
	}

	// 通过 CDP SetCookies 注入
	err = sb.browser.SetCookies(proto.CookiesToParams(cks))
	if err != nil {
		logrus.Warnf("[browser] 注入 cookies 失败: %v", err)
		return
	}

	logrus.Infof("[browser] 已从 %s 加载 %d 个 cookies", cookiePath, len(cks))

	// 预热 session：导航到小红书主站和 creator 平台
	// 让浏览器带着 cookies 访问，触发 SSO 恢复 localStorage/sessionStorage
	sb.warmupSession()
}

// warmupSession 导航到小红书主站触发 SSO 恢复完整 session 状态
func (sb *SharedBrowser) warmupSession() {
	logrus.Info("[browser] 开始预热 session（恢复 localStorage/sessionStorage）...")

	page := stealth.MustPage(sb.browser)
	defer page.MustClose()

	// 1. 先访问主站，让 SSO cookies 建立 session
	err := page.Navigate("https://www.xiaohongshu.com")
	if err != nil {
		logrus.Warnf("[browser] 预热: 访问主站失败: %v", err)
		return
	}
	page.MustWaitLoad()
	logrus.Info("[browser] 预热: 主站已加载")

	// 等待 SSO 完成
	time.Sleep(3 * time.Second)

	// 2. 访问 creator 平台，让 creator 的 session 也建立
	err = page.Navigate("https://creator.xiaohongshu.com")
	if err != nil {
		logrus.Warnf("[browser] 预热: 访问 creator 平台失败: %v", err)
		return
	}
	page.MustWaitLoad()
	time.Sleep(3 * time.Second)

	// 检查是否成功登录到 creator
	currentURL := page.MustInfo().URL
	logrus.Infof("[browser] 预热完成: creator URL = %s", currentURL)

	// 如果已经重定向到登录页，说明 cookies 过期了
	if strings.Contains(currentURL, "login") || strings.Contains(currentURL, "sign") {
		logrus.Warn("[browser] 预热: cookies 已过期，需要重新扫码登录")
	} else {
		logrus.Info("[browser] 预热: session 恢复成功 ✅")
	}
}

// NewPage 创建一个新的 stealth 页面（新 tab）
func (sb *SharedBrowser) NewPage() *rod.Page {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return stealth.MustPage(sb.browser)
}

// GetBrowser 返回底层的 rod.Browser
func (sb *SharedBrowser) GetBrowser() *rod.Browser {
	return sb.browser
}

// Close 关闭浏览器（通常只在程序退出时调用）
func (sb *SharedBrowser) Close() {
	globalMu.Lock()
	defer globalMu.Unlock()

	if sb.browser != nil {
		sb.browser.MustClose()
	}
	globalBrowser = nil
	logrus.Info("[browser] 全局共享浏览器已关闭")
}
