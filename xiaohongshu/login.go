package xiaohongshu

import (
	"context"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type LoginAction struct {
	page *rod.Page
}

func NewLogin(page *rod.Page) *LoginAction {
	return &LoginAction{page: page}
}

// loginDialogSelectors 登录弹窗/二维码弹窗的选择器
var loginDialogSelectors = []string{
	`.login-container`,
	`.qrcode-img`,
	`img[class*="qrcode"]`,
	`[class*="login-modal"]`,
	`[class*="login-overlay"]`,
}

func (a *LoginAction) isLoggedIn(pp *rod.Page) bool {
	// 如果存在登录弹窗/二维码，肯定未登录
	for _, sel := range loginDialogSelectors {
		if exists, _, _ := pp.Has(sel); exists {
			logrus.Debugf("login dialog found: %s, NOT logged in", sel)
			return false
		}
	}

	// 使用原始的精确选择器检测已登录状态
	if exists, _, _ := pp.Has(`.main-container .user .link-wrapper .channel`); exists {
		logrus.Debug("login detected via original selector")
		return true
	}

	// 检查 URL — 如果被重定向到 /login 页面，肯定未登录
	info, err := pp.Info()
	if err == nil {
		url := info.URL
		if strings.Contains(url, "/login") || strings.Contains(url, "redirectReason") {
			logrus.Debugf("login page URL detected: %s, NOT logged in", url)
			return false
		}
	}

	// 尝试通过 JavaScript 检查 cookies（注意 httpOnly cookies 不可见）
	result, err := pp.Eval(`() => {
		try {
			// 检查页面中是否有用户相关的全局状态
			if (window.__INITIAL_STATE__ && window.__INITIAL_STATE__.user && window.__INITIAL_STATE__.user.id) {
				return true;
			}
		} catch(e) {}
		return false;
	}`)
	if err == nil && result != nil && result.Value.Bool() {
		logrus.Debug("login detected via __INITIAL_STATE__")
		return true
	}

	return false
}

func (a *LoginAction) CheckLoginStatus(ctx context.Context) (bool, error) {
	pp := a.page.Context(ctx)
	pp.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	time.Sleep(2 * time.Second)
	DismissCookieConsent(pp)
	time.Sleep(500 * time.Millisecond)
	return a.isLoggedIn(pp), nil
}

func (a *LoginAction) Login(ctx context.Context) error {
	pp := a.page.Context(ctx)

	// 导航到小红书首页，这会触发登录弹窗
	// 使用共享浏览器单例时，登录后 SSO 自动在 creator 等子站生效
	pp.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	time.Sleep(2 * time.Second)
	DismissCookieConsent(pp)
	time.Sleep(500 * time.Millisecond)

	if a.isLoggedIn(pp) {
		return nil
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return errors.New("login timeout")
		case <-ticker.C:
			if a.isLoggedIn(pp) {
				return nil
			}
		}
	}
}

// isCreatorLoggedIn 检查创作者平台是否已登录
// 登录成功后 creator 平台会自动跳转到首页或其他页面，不再停留在 /login
func (a *LoginAction) isCreatorLoggedIn(pp *rod.Page) bool {
	info, err := pp.Info()
	if err != nil {
		return false
	}
	url := info.URL
	// 如果 URL 不包含 /login，说明已经登录成功并跳转了
	if strings.Contains(url, "creator.xiaohongshu.com") && !strings.Contains(url, "/login") {
		logrus.Debugf("creator platform logged in, URL: %s", url)
		return true
	}
	return false
}
func (a *LoginAction) FetchQrcodeImage(ctx context.Context) (string, bool, error) {
	pp := a.page.Context(ctx)
	pp.MustNavigate("https://www.xiaohongshu.com/explore").MustWaitLoad()
	time.Sleep(2 * time.Second)
	DismissCookieConsent(pp)
	time.Sleep(500 * time.Millisecond)

	if a.isLoggedIn(pp) {
		return "", true, nil
	}

	// 获取二维码图片 - 尝试多个选择器
	qrSelectors := []string{
		`.login-container .qrcode-img`,
		`.qrcode-img`,
		`img[class*="qrcode"]`,
		`.login-container img`,
	}

	for _, sel := range qrSelectors {
		el, err := pp.Element(sel)
		if err != nil {
			continue
		}
		src, err := el.Attribute("src")
		if err != nil || src == nil || len(*src) == 0 {
			continue
		}
		logrus.Infof("found QR code via selector: %s", sel)
		return *src, false, nil
	}

	return "", false, errors.New("qrcode image not found with any selector")
}

func (a *LoginAction) WaitForLogin(ctx context.Context) bool {
	pp := a.page.Context(ctx)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	logrus.Info("waiting for QR code scan...")

	for {
		select {
		case <-ctx.Done():
			logrus.Warn("WaitForLogin: context timeout")
			return false
		case <-ticker.C:
			if a.isLoggedIn(pp) {
				logrus.Info("login detected successfully!")
				return true
			}
			// 检查登录弹窗是否消失（扫码成功后弹窗会关闭）
			if hasLogin, _, _ := pp.Has(`.login-container`); !hasLogin {
				time.Sleep(2 * time.Second)
				if a.isLoggedIn(pp) {
					logrus.Info("login detected after popup disappeared!")
					return true
				}
			}
		}
	}
}
