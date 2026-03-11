package xiaohongshu

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// PublishImageContent 发布图文内容
type PublishImageContent struct {
	Title        string
	Content      string
	Tags         []string
	ImagePaths   []string
	ScheduleTime *time.Time // 定时发布时间，nil 表示立即发布
	IsOriginal   bool       // 是否声明原创
	Visibility   string     // 可见范围: "公开可见"(默认), "仅自己可见", "仅互关好友可见"
}

type PublishAction struct {
	page *rod.Page
}

const (
	urlOfPublic = `https://creator.xiaohongshu.com/publish/publish?source=official`
)

func NewPublishImageAction(page *rod.Page) (*PublishAction, error) {

	pp := page.Timeout(300 * time.Second)

	logrus.Info("[publish] 导航到发布页面...")
	// 使用更稳健的导航和等待策略
	if err := pp.Navigate(urlOfPublic); err != nil {
		return nil, errors.Wrap(err, "导航到发布页面失败")
	}
	logrus.Info("[publish] Navigate 完成，等待页面加载...")

	// 等待页面加载，使用 WaitLoad 代替 WaitIdle（更宽松）
	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("等待页面加载出现问题: %v，继续尝试", err)
	}
	logrus.Info("[publish] WaitLoad 完成")
	time.Sleep(2 * time.Second)

	// 处理海外服务器的 GDPR Cookie Consent 弹窗
	DismissCookieConsent(pp)
	time.Sleep(500 * time.Millisecond)

	// 截图查看页面状态
	screenshotData, scrErr := pp.Screenshot(true, nil)
	if scrErr == nil && len(screenshotData) > 0 {
		os.WriteFile("/tmp/xhs_navigate_debug.png", screenshotData, 0644)
		logrus.Info("[publish] 已保存导航后截图到 /tmp/xhs_navigate_debug.png")
	} else if scrErr != nil {
		logrus.Warnf("[publish] 截图失败: %v", scrErr)
	}

	// 检查当前 URL，看是否被重定向到登录页
	currentURL := pp.MustInfo().URL
	logrus.Infof("[publish] 当前页面 URL: %s", currentURL)

	// 等待页面稳定
	if err := pp.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待 DOM 稳定出现问题: %v，继续尝试", err)
	}
	logrus.Info("[publish] DOM 稳定，点击上传图文 TAB...")
	time.Sleep(1 * time.Second)

	if err := mustClickPublishTab(pp, "上传图文"); err != nil {
		logrus.Errorf("点击上传图文 TAB 失败: %v", err)
		return nil, err
	}
	logrus.Info("[publish] 上传图文 TAB 已点击")

	time.Sleep(1 * time.Second)

	return &PublishAction{
		page: pp,
	}, nil
}

func (p *PublishAction) Publish(ctx context.Context, content PublishImageContent) error {
	if len(content.ImagePaths) == 0 {
		return errors.New("图片不能为空")
	}

	page := p.page.Context(ctx)

	if err := uploadImages(page, content.ImagePaths); err != nil {
		return errors.Wrap(err, "小红书上传图片失败")
	}

	tags := content.Tags
	if len(tags) >= 10 {
		logrus.Warnf("标签数量超过10，截取前10个标签")
		tags = tags[:10]
	}

	logrus.Infof("发布内容: title=%s, images=%v, tags=%v, schedule=%v, original=%v, visibility=%s", content.Title, len(content.ImagePaths), tags, content.ScheduleTime, content.IsOriginal, content.Visibility)

	if err := submitPublish(page, content.Title, content.Content, tags, content.ScheduleTime, content.IsOriginal, content.Visibility); err != nil {
		return errors.Wrap(err, "小红书发布失败")
	}

	return nil
}

func removePopCover(page *rod.Page) {

	// 先移除弹窗封面
	has, elem, err := page.Has("div.d-popover")
	if err != nil {
		return
	}
	if has {
		elem.MustRemove()
	}

	// 兜底：点击一下空位置吧
	clickEmptyPosition(page)
}

func clickEmptyPosition(page *rod.Page) {
	x := 380 + rand.Intn(100)
	y := 20 + rand.Intn(60)
	page.Mouse.MustMoveTo(float64(x), float64(y)).MustClick(proto.InputMouseButtonLeft)
}

// DismissCookieConsent 自动关闭海外服务器上的 GDPR Cookie 同意弹窗
// 小红书在海外 IP 访问时会弹出 "Your Cookie Preferences" 对话框，
// 必须点击 "Accept all cookies" 才能继续操作，否则页面被遮挡无法交互。
func DismissCookieConsent(page *rod.Page) {
	page.MustEval(`() => {
		// 查找并点击 "Accept all cookies" 按钮
		const buttons = document.querySelectorAll('button');
		for (const btn of buttons) {
			const text = btn.textContent.trim().toLowerCase();
			if (text.includes('accept all') || text.includes('accept cookies') || 
				text.includes('接受所有') || text.includes('全部接受')) {
				btn.click();
				console.log('Cookie consent dismissed:', text);
				return true;
			}
		}
		// 尝试通过 class/id 查找
		const acceptBtns = document.querySelectorAll(
			'[class*="accept"], [class*="cookie-accept"], [id*="accept"], ' +
			'[class*="consent"] button, [class*="cookie"] button'
		);
		for (const btn of acceptBtns) {
			const text = btn.textContent.trim().toLowerCase();
			if (text.includes('accept') || text.includes('接受')) {
				btn.click();
				console.log('Cookie consent dismissed via class:', text);
				return true;
			}
		}
		// 尝试移除 cookie 同意弹窗的覆盖层
		const overlays = document.querySelectorAll(
			'[class*="cookie-banner"], [class*="cookie-consent"], ' +
			'[class*="cookie-overlay"], [class*="consent-banner"], ' +
			'[class*="CookiePreferences"], [id*="cookie"]'
		);
		overlays.forEach(el => el.remove());
		return false;
	}`)
	logrus.Debug("[cookie-consent] attempted to dismiss cookie consent popup")
}

func mustClickPublishTab(page *rod.Page, tabname string) error {
	page.MustElement(`div.upload-content`).MustWaitVisible()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		tab, blocked, err := getTabElement(page, tabname)
		if err != nil {
			logrus.Warnf("获取发布 TAB 元素失败: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if tab == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if blocked {
			logrus.Info("发布 TAB 被遮挡，尝试移除遮挡")
			removePopCover(page)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if err := tab.Click(proto.InputMouseButtonLeft, 1); err != nil {
			logrus.Warnf("点击发布 TAB 失败: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		return nil
	}

	return errors.Errorf("没有找到发布 TAB - %s", tabname)
}

func getTabElement(page *rod.Page, tabname string) (*rod.Element, bool, error) {
	elems, err := page.Elements("div.creator-tab")
	if err != nil {
		return nil, false, err
	}

	for _, elem := range elems {
		if !isElementVisible(elem) {
			continue
		}

		text, err := elem.Text()
		if err != nil {
			logrus.Debugf("获取发布 TAB 文本失败: %v", err)
			continue
		}

		if strings.TrimSpace(text) != tabname {
			continue
		}

		blocked, err := isElementBlocked(elem)
		if err != nil {
			return nil, false, err
		}

		return elem, blocked, nil
	}

	return nil, false, nil
}

func isElementBlocked(elem *rod.Element) (bool, error) {
	result, err := elem.Eval(`() => {
		const rect = this.getBoundingClientRect();
		if (rect.width === 0 || rect.height === 0) {
			return true;
		}
		const x = rect.left + rect.width / 2;
		const y = rect.top + rect.height / 2;
		const target = document.elementFromPoint(x, y);
		return !(target === this || this.contains(target));
	}`)
	if err != nil {
		return false, err
	}

	return result.Value.Bool(), nil
}

func uploadImages(page *rod.Page, imagesPaths []string) error {
	// 验证文件路径有效性
	validPaths := make([]string, 0, len(imagesPaths))
	for _, path := range imagesPaths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			logrus.Warnf("图片文件不存在: %s", path)
			continue
		}
		validPaths = append(validPaths, path)
		logrus.Infof("获取有效图片：%s", path)
	}

	// 逐张上传：每张上传后等待预览出现，再上传下一张
	for i, path := range validPaths {
		selector := `input[type="file"]`
		if i == 0 {
			selector = ".upload-input"
		}

		uploadInput, err := page.Element(selector)
		if err != nil {
			return errors.Wrapf(err, "查找上传输入框失败(第%d张)", i+1)
		}
		if err := uploadInput.SetFiles([]string{path}); err != nil {
			return errors.Wrapf(err, "上传第%d张图片失败", i+1)
		}

		slog.Info("图片已提交上传", "index", i+1, "path", path)

		// 等待当前图片上传完成（预览元素数量达到 i+1），最多等 60 秒
		if err := waitForUploadComplete(page, i+1); err != nil {
			return errors.Wrapf(err, "第%d张图片上传超时", i+1)
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}

// waitForUploadComplete 等待第 expectedCount 张图片真正上传完成
// 先等预览元素出现（文件被选中），然后等上传进度完成（进度条消失、loading 状态结束）
func waitForUploadComplete(page *rod.Page, expectedCount int) error {
	maxWaitTime := 120 * time.Second // 海外服务器可能需要更长时间
	checkInterval := 1 * time.Second
	start := time.Now()
	lastLogCount := expectedCount - 1

	// Phase 1: 等待预览元素出现
	for time.Since(start) < maxWaitTime {
		uploadedImages, err := page.Elements(".img-preview-area .pr")
		if err != nil {
			time.Sleep(checkInterval)
			continue
		}

		currentCount := len(uploadedImages)
		if currentCount != lastLogCount {
			slog.Info("等待图片上传", "current", currentCount, "expected", expectedCount)
			lastLogCount = currentCount
		}
		if currentCount >= expectedCount {
			slog.Info("图片预览已出现", "count", currentCount)
			break
		}

		time.Sleep(checkInterval)
	}

	// Phase 2: 图片预览已出现，等待上传完成
	// 注意: 之前的 JS DOM 检测（进度条、loading 类名、blob URL）实际上不匹配小红书的 DOM 结构，
	// 导致永远返回 false，白等 90 秒。
	// 最终发布是否成功由 submitPublish 中的 published=true URL 检查来保障，
	// 这里只需等待一个合理的固定时间让图片上传到 CDN 即可。
	fixedWait := 30 * time.Second
	slog.Info("图片预览已出现，等待上传完成...", "wait_seconds", int(fixedWait.Seconds()))
	time.Sleep(fixedWait)
	slog.Info("图片上传等待完成")

	return nil
}

func submitPublish(page *rod.Page, title, content string, tags []string, scheduleTime *time.Time, isOriginal bool, visibility string) error {
	titleElem, err := page.Element("div.d-input input")
	if err != nil {
		return errors.Wrap(err, "查找标题输入框失败")
	}
	if err := titleElem.Input(title); err != nil {
		return errors.Wrap(err, "输入标题失败")
	}

	// 检查标题长度
	time.Sleep(500 * time.Millisecond)
	if err := checkTitleMaxLength(page); err != nil {
		return err
	}
	slog.Info("检查标题长度：通过")

	time.Sleep(1 * time.Second)

	contentElem, ok := getContentElement(page)
	if !ok {
		return errors.New("没有找到内容输入框")
	}
	if err := contentElem.Input(content); err != nil {
		return errors.Wrap(err, "输入正文失败")
	}
	if err := inputTags(contentElem, tags); err != nil {
		return err
	}

	time.Sleep(1 * time.Second)

	// 检查正文长度
	if err := checkContentMaxLength(page); err != nil {
		return err
	}
	slog.Info("检查正文长度：通过")

	// 处理定时发布
	if scheduleTime != nil {
		if err := setSchedulePublish(page, *scheduleTime); err != nil {
			return errors.Wrap(err, "设置定时发布失败")
		}
		slog.Info("定时发布设置完成", "schedule_time", scheduleTime.Format("2006-01-02 15:04"))
	}

	// 设置可见范围
	slog.Info("[debug] 准备设置可见范围", "visibility", visibility)
	if err := setVisibility(page, visibility); err != nil {
		return errors.Wrap(err, "设置可见范围失败")
	}
	slog.Info("[debug] 可见范围设置完成")

	// 处理原创声明
	if isOriginal {
		if err := setOriginal(page); err != nil {
			slog.Warn("设置原创声明失败，继续发布", "error", err)
		} else {
			slog.Info("已声明原创")
		}
	}

	slog.Info("[debug] 准备查找发布按钮")

	// 关闭可能存在的标签联想弹窗（输入#后弹出的下拉框会遮挡发布按钮）
	page.MustEval(`() => {
		// 移除标签联想下拉框
		const containers = document.querySelectorAll('#creator-editor-topic-container, .topic-container, .d-options-wrapper');
		containers.forEach(c => c.remove());
		// 点击空白处关闭任何弹窗
		document.body.click();
	}`)
	time.Sleep(500 * time.Millisecond)
	// 按 Escape 关闭残留弹窗
	page.KeyActions().Press(input.Escape).MustDo()
	time.Sleep(500 * time.Millisecond)
	clickEmptyPosition(page)
	time.Sleep(500 * time.Millisecond)
	slog.Info("[debug] 已清理弹窗")

	// 截图以便调试
	screenshotData, scrErr := page.Screenshot(true, nil)
	if scrErr == nil && len(screenshotData) > 0 {
		os.WriteFile("/tmp/xhs_publish_debug.png", screenshotData, 0644)
		slog.Info("[debug] 已保存发布页面截图到 /tmp/xhs_publish_debug.png")
	}

	// 枚举所有发布相关按钮用于调试
	btnListRaw := page.MustEval(`() => {
		const buttons = document.querySelectorAll('button');
		const publishBtns = [];
		buttons.forEach((btn, i) => {
			if (btn.textContent.trim().includes('发布')) {
				const rect = btn.getBoundingClientRect();
				publishBtns.push({
					index: i,
					text: btn.textContent.trim().substring(0, 30),
					class: btn.className.substring(0, 80),
					x: Math.round(rect.x),
					y: Math.round(rect.y),
					w: Math.round(rect.width),
					h: Math.round(rect.height),
				});
			}
		});
		return publishBtns;
	}`)
	slog.Info("[debug] 页面上所有发布按钮", "buttons", btnListRaw.String())

	// 通过 Rod 原生 Element.Click() 点击发布按钮
	// JS .click() / dispatchEvent 在长内容页面上不可靠，
	// Rod 原生点击通过 CDP Input.dispatchMouseEvent 发送真实鼠标事件
	publishBtn, err := page.Element(`button`)
	var foundBtn *rod.Element
	if err == nil {
		// 遍历所有 button 找到文本恰好是"发布"的最底部按钮
		buttons, _ := page.Elements(`button`)
		maxY := 0.0
		for _, btn := range buttons {
			text, _ := btn.Text()
			text = strings.TrimSpace(text)
			if text != "发布" {
				continue
			}
			box, boxErr := btn.Shape()
			if boxErr != nil || len(box.Quads) == 0 {
				continue
			}
			// Y 坐标取第一个点的 Y
			y := box.Quads[0][1]
			width := box.Quads[0][2] - box.Quads[0][0]
			if y > maxY && width > 80 {
				maxY = y
				foundBtn = btn
			}
		}
		_ = publishBtn // suppress unused
	}

	if foundBtn != nil {
		slog.Info("[debug] 找到发布按钮，使用多重点击策略")

		// 先隐藏右侧预览面板和任何可能遮挡按钮的 overlay
		page.MustEval(`() => {
			// 隐藏右侧预览面板（可能有透明 overlay 拦截鼠标事件）
			const preview = document.querySelector('.note-preview, .preview-container, .preview-panel, [class*="preview"]');
			if (preview) {
				preview.style.display = 'none';
				console.log('[publish] 已隐藏预览面板');
			}
			// 移除所有可能的 overlay/mask
			document.querySelectorAll('.d-overlay, .d-mask, [class*="overlay"], [class*="mask"]').forEach(el => {
				el.remove();
			});
		}`)
		time.Sleep(300 * time.Millisecond)

		// scrollIntoView 确保按钮在视口中
		foundBtn.MustEval(`() => this.scrollIntoView({block: 'center'})`)
		time.Sleep(500 * time.Millisecond)

		// 策略1: Rod 原生点击 — 通过 CDP 发送真实鼠标事件
		clickErr := foundBtn.Click(proto.InputMouseButtonLeft, 1)
		if clickErr != nil {
			slog.Warn("[debug] Rod 原生点击失败", "err", clickErr)
		} else {
			slog.Info("[debug] Rod 原生点击完成")
		}
		time.Sleep(500 * time.Millisecond)

		// 策略2: 无论 Rod 原生点击是否成功，都用 JS click() 作为双保险
		// JS click() 不受 overlay/遮挡影响，直接触发 DOM 事件
		foundBtn.MustEval(`() => {
			this.click();
			this.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, view: window}));
		}`)
		slog.Info("[debug] JS click() 已执行（双保险）")
	} else {
		slog.Warn("[debug] 未找到发布按钮 Element，回退到 JS 点击")
		page.MustEval(`() => {
			const buttons = document.querySelectorAll('button');
			let targetBtn = null;
			let maxY = 0;
			buttons.forEach(btn => {
				const text = btn.textContent.trim();
				const rect = btn.getBoundingClientRect();
				if (text === '发布' && rect.y > maxY && rect.width > 80 && rect.height > 20) {
					maxY = rect.y;
					targetBtn = btn;
				}
			});
			if (targetBtn) {
				targetBtn.scrollIntoView({block: 'center'});
				targetBtn.click();
			}
		}`)
	}

	// 等待可能的确认弹窗（如"确认发布？"）
	time.Sleep(2 * time.Second)
	page.MustEval(`() => {
		const allBtns = document.querySelectorAll('button, .d-button, [role="button"]');
		for (const btn of allBtns) {
			const text = btn.textContent.trim();
			if (text === '确认发布' || text === '确认' || text === '确定') {
				btn.click();
				return text;
			}
		}
		return '';
	}`)

	// 等待发布响应（10 秒）
	time.Sleep(10 * time.Second)

	// 点击后截图看结果
	screenshotData3, _ := page.Screenshot(true, nil)
	if len(screenshotData3) > 0 {
		os.WriteFile("/tmp/xhs_after_click.png", screenshotData3, 0644)
		slog.Info("[debug] 已保存点击后截图到 /tmp/xhs_after_click.png")
	}

	// 检查当前URL - 发布成功后通常会跳转到 published=true
	finalURL := page.MustInfo().URL
	slog.Info("[debug] 发布后页面URL", "url", finalURL)

	// 检查是否真正发布成功
	if strings.Contains(finalURL, "published=true") {
		slog.Info("[debug] ✅ 发布成功确认: URL 包含 published=true")
		return nil
	}

	// URL 不包含 published=true，说明发布未成功
	slog.Warn("[debug] ❌ 发布失败: URL 未包含 published=true", "url", finalURL)
	return errors.Errorf("发布未成功: 页面URL未跳转到published=true (当前URL: %s)", finalURL)
}

// 检查标题是否超过最大长度
func checkTitleMaxLength(page *rod.Page) error {
	has, elem, err := page.Has(`div.title-container div.max_suffix`)
	if err != nil {
		return errors.Wrap(err, "检查标题长度元素失败")
	}

	// 元素不存在，说明标题没超长
	if !has {
		return nil
	}

	// 元素存在，说明标题超长
	titleLength, err := elem.Text()
	if err != nil {
		return errors.Wrap(err, "获取标题长度文本失败")
	}

	return makeMaxLengthError(titleLength)
}

func checkContentMaxLength(page *rod.Page) error {
	has, elem, err := page.Has(`div.edit-container div.length-error`)
	if err != nil {
		return errors.Wrap(err, "检查正文长度元素失败")
	}

	// 元素不存在，说明正文没超长
	if !has {
		return nil
	}

	// 元素存在，说明正文超长
	contentLength, err := elem.Text()
	if err != nil {
		return errors.Wrap(err, "获取正文长度文本失败")
	}

	return makeMaxLengthError(contentLength)
}

func makeMaxLengthError(elemText string) error {
	parts := strings.Split(elemText, "/")
	if len(parts) != 2 {
		return errors.Errorf("长度超过限制: %s", elemText)
	}

	currLen, maxLen := parts[0], parts[1]

	return errors.Errorf("当前输入长度为%s，最大长度为%s", currLen, maxLen)
}

// 查找内容输入框 - 使用Race方法处理两种样式
func getContentElement(page *rod.Page) (*rod.Element, bool) {
	var foundElement *rod.Element
	var found bool

	page.Race().
		Element("div.ql-editor").MustHandle(func(e *rod.Element) {
		foundElement = e
		found = true
	}).
		ElementFunc(func(page *rod.Page) (*rod.Element, error) {
			return findTextboxByPlaceholder(page)
		}).MustHandle(func(e *rod.Element) {
		foundElement = e
		found = true
	}).
		MustDo()

	if found {
		return foundElement, true
	}

	slog.Warn("no content element found by any method")
	return nil, false
}

func inputTags(contentElem *rod.Element, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	time.Sleep(1 * time.Second)

	for i := 0; i < 20; i++ {
		ka, err := contentElem.KeyActions()
		if err != nil {
			return errors.Wrap(err, "创建键盘操作失败")
		}
		if err := ka.Type(input.ArrowDown).Do(); err != nil {
			return errors.Wrap(err, "按下方向键失败")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ka, err := contentElem.KeyActions()
	if err != nil {
		return errors.Wrap(err, "创建键盘操作失败")
	}
	if err := ka.Press(input.Enter).Press(input.Enter).Do(); err != nil {
		return errors.Wrap(err, "按下回车键失败")
	}

	time.Sleep(1 * time.Second)

	for _, tag := range tags {
		tag = strings.TrimLeft(tag, "#")
		if err := inputTag(contentElem, tag); err != nil {
			return errors.Wrapf(err, "输入标签[%s]失败", tag)
		}
	}
	return nil
}

func inputTag(contentElem *rod.Element, tag string) error {
	if err := contentElem.Input("#"); err != nil {
		return errors.Wrap(err, "输入#失败")
	}
	time.Sleep(200 * time.Millisecond)

	for _, char := range tag {
		if err := contentElem.Input(string(char)); err != nil {
			return errors.Wrapf(err, "输入字符[%c]失败", char)
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(1 * time.Second)

	page := contentElem.Page()
	topicContainer, err := page.Element("#creator-editor-topic-container")
	if err != nil || topicContainer == nil {
		slog.Warn("未找到标签联想下拉框，直接输入空格", "tag", tag)
		return contentElem.Input(" ")
	}

	firstItem, err := topicContainer.Element(".item")
	if err != nil || firstItem == nil {
		slog.Warn("未找到标签联想选项，直接输入空格", "tag", tag)
		return contentElem.Input(" ")
	}

	if err := firstItem.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return errors.Wrap(err, "点击标签联想选项失败")
	}
	slog.Info("成功点击标签联想选项", "tag", tag)
	time.Sleep(200 * time.Millisecond)

	time.Sleep(500 * time.Millisecond) // 等待标签处理完成
	return nil
}

func findTextboxByPlaceholder(page *rod.Page) (*rod.Element, error) {
	elements := page.MustElements("p")
	if elements == nil {
		return nil, errors.New("no p elements found")
	}

	// 查找包含指定placeholder的元素
	placeholderElem := findPlaceholderElement(elements, "输入正文描述")
	if placeholderElem == nil {
		return nil, errors.New("no placeholder element found")
	}

	// 向上查找textbox父元素
	textboxElem := findTextboxParent(placeholderElem)
	if textboxElem == nil {
		return nil, errors.New("no textbox parent found")
	}

	return textboxElem, nil
}

func findPlaceholderElement(elements []*rod.Element, searchText string) *rod.Element {
	for _, elem := range elements {
		placeholder, err := elem.Attribute("data-placeholder")
		if err != nil || placeholder == nil {
			continue
		}

		if strings.Contains(*placeholder, searchText) {
			return elem
		}
	}
	return nil
}

func findTextboxParent(elem *rod.Element) *rod.Element {
	currentElem := elem
	for i := 0; i < 5; i++ {
		parent, err := currentElem.Parent()
		if err != nil {
			break
		}

		role, err := parent.Attribute("role")
		if err != nil || role == nil {
			currentElem = parent
			continue
		}

		if *role == "textbox" {
			return parent
		}

		currentElem = parent
	}
	return nil
}

// isElementVisible 检查元素是否可见
func isElementVisible(elem *rod.Element) bool {

	// 检查是否有隐藏样式
	style, err := elem.Attribute("style")
	if err == nil && style != nil {
		styleStr := *style

		if strings.Contains(styleStr, "left: -9999px") ||
			strings.Contains(styleStr, "top: -9999px") ||
			strings.Contains(styleStr, "position: absolute; left: -9999px") ||
			strings.Contains(styleStr, "display: none") ||
			strings.Contains(styleStr, "visibility: hidden") {
			return false
		}
	}

	visible, err := elem.Visible()
	if err != nil {
		slog.Warn("无法获取元素可见性", "error", err)
		return true
	}

	return visible
}

// setVisibility 设置可见范围
// 支持: "公开可见"(默认), "仅自己可见", "仅互关好友可见"
func setVisibility(page *rod.Page, visibility string) error {
	if visibility == "" || visibility == "公开可见" {
		slog.Info("可见范围使用默认：公开可见")
		return nil
	}

	// 支持的选项校验
	supported := map[string]bool{"仅自己可见": true, "仅互关好友可见": true}
	if !supported[visibility] {
		return errors.Errorf("不支持的可见范围: %s，支持: 公开可见、仅自己可见、仅互关好友可见", visibility)
	}

	// 点击可见范围下拉框
	dropdown, err := page.Element("div.permission-card-wrapper div.d-select-content")
	if err != nil {
		return errors.Wrap(err, "查找可见范围下拉框失败")
	}
	if err := dropdown.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return errors.Wrap(err, "点击可见范围下拉框失败")
	}
	time.Sleep(500 * time.Millisecond)

	// 在弹窗中查找并点击目标选项
	opts, err := page.Elements("div.d-options-wrapper div.d-grid-item div.custom-option")
	if err != nil {
		return errors.Wrap(err, "查找可见范围选项失败")
	}
	for _, opt := range opts {
		text, err := opt.Text()
		if err != nil {
			continue
		}
		if strings.Contains(text, visibility) {
			if err := opt.Click(proto.InputMouseButtonLeft, 1); err != nil {
				return errors.Wrap(err, "选择可见范围失败")
			}
			slog.Info("已设置可见范围", "visibility", visibility)
			time.Sleep(200 * time.Millisecond)
			return nil
		}
	}
	return errors.Errorf("未找到可见范围选项: %s", visibility)
}

// setSchedulePublish 设置定时发布时间
func setSchedulePublish(page *rod.Page, t time.Time) error {
	// 1. 点击定时发布开关
	if err := clickScheduleSwitch(page); err != nil {
		return err
	}
	time.Sleep(800 * time.Millisecond)

	// 2. 设置日期时间
	if err := setDateTime(page, t); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)

	return nil
}

// clickScheduleSwitch 点击定时发布开关
func clickScheduleSwitch(page *rod.Page) error {
	switchElem, err := page.Element(".post-time-wrapper .d-switch")
	if err != nil {
		return errors.Wrap(err, "查找定时发布开关失败")
	}

	if err := switchElem.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return errors.Wrap(err, "点击定时发布开关失败")
	}
	slog.Info("已点击定时发布开关")
	return nil
}

// setDateTime 设置日期时间
func setDateTime(page *rod.Page, t time.Time) error {
	dateTimeStr := t.Format("2006-01-02 15:04")

	input, err := page.Element(".date-picker-container input")
	if err != nil {
		return errors.Wrap(err, "查找日期时间输入框失败")
	}

	if err := input.SelectAllText(); err != nil {
		return errors.Wrap(err, "选择日期时间文本失败")
	}
	if err := input.Input(dateTimeStr); err != nil {
		return errors.Wrap(err, "输入日期时间失败")
	}
	slog.Info("已设置日期时间", "datetime", dateTimeStr)

	return nil
}

// setOriginal 设置原创声明
func setOriginal(page *rod.Page) error {
	// 根据小红书创作者页面的实际结构：
	// div.custom-switch-card 包含 span.has-tips 文本为"原创声明"
	// 开关是 div.d-switch 组件

	// 查找包含"原创声明"文本的 custom-switch-card
	switchCards, err := page.Elements("div.custom-switch-card")
	if err != nil {
		return errors.Wrap(err, "查找原创声明卡片失败")
	}

	for _, card := range switchCards {
		text, err := card.Text()
		if err != nil {
			continue
		}

		// 检查是否是原创声明卡片
		if !strings.Contains(text, "原创声明") {
			continue
		}

		// 找到原创声明卡片，查找其中的 d-switch
		switchElem, err := card.Element("div.d-switch")
		if err != nil {
			continue
		}

		// 检查开关是否已打开
		checked, err := switchElem.Eval(`() => {
			const input = this.querySelector('input[type="checkbox"]');
			return input ? input.checked : false;
		}`)
		if err != nil {
			continue
		}

		if checked.Value.Bool() {
			slog.Info("原创声明已开启")
			return nil
		}

		// 点击开关
		if err := switchElem.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return errors.Wrap(err, "点击原创声明开关失败")
		}

		time.Sleep(500 * time.Millisecond)

		// 处理原创声明确认弹窗
		if err := confirmOriginalDeclaration(page); err != nil {
			return errors.Wrap(err, "确认原创声明失败")
		}

		slog.Info("已开启原创声明")
		return nil
	}

	return errors.New("未找到原创声明选项")
}

// confirmOriginalDeclaration 处理原创声明确认弹窗
func confirmOriginalDeclaration(page *rod.Page) error {
	// 等待确认弹窗出现
	time.Sleep(800 * time.Millisecond)

	// 使用 JavaScript 直接处理弹窗，更可靠
	result, err := page.Eval(`
		() => {
			// 查找包含"原创声明须知"的 footer 区域
			const footers = document.querySelectorAll('div.footer');
			for (const footer of footers) {
				// 检查是否包含原创声明相关内容
				if (!footer.textContent.includes('原创声明须知')) {
					continue;
				}

				// 找到 checkbox 并勾选
				const checkbox = footer.querySelector('div.d-checkbox input[type="checkbox"]');
				if (checkbox && !checkbox.checked) {
					checkbox.click();
					console.log('已勾选原创声明须知 checkbox');
				}

				// 等待一下让按钮变为可用
				return 'found_footer';
			}
			return 'footer_not_found';
		}
	`)
	if err != nil {
		slog.Warn("执行查找弹窗脚本失败", "error", err)
	} else if result.Value.String() == "footer_not_found" {
		slog.Warn("未找到原创声明确认弹窗的 footer")
	}

	time.Sleep(500 * time.Millisecond)

	// 再次使用 JavaScript 点击声明原创按钮
	result2, err := page.Eval(`
		() => {
			const footers = document.querySelectorAll('div.footer');
			for (const footer of footers) {
				if (!footer.textContent.includes('声明原创')) {
					continue;
				}

				// 找到声明原创按钮
				const btn = footer.querySelector('button.custom-button');
				if (btn) {
					// 检查是否禁用
					if (btn.classList.contains('disabled') || btn.disabled) {
						// 尝试再次勾选 checkbox
						const checkbox = footer.querySelector('div.d-checkbox input[type="checkbox"]');
						if (checkbox && !checkbox.checked) {
							checkbox.click();
						}
						return 'button_disabled';
					}
					btn.click();
					return 'clicked';
				}
			}
			return 'button_not_found';
		}
	`)
	if err != nil {
		return errors.Wrap(err, "执行点击按钮脚本失败")
	}

	status := result2.Value.String()
	slog.Info("原创声明确认结果", "status", status)

	if status == "button_not_found" {
		return errors.New("未找到声明原创按钮")
	}
	if status == "button_disabled" {
		return errors.New("声明原创按钮仍处于禁用状态")
	}

	slog.Info("已成功点击声明原创按钮")
	time.Sleep(300 * time.Millisecond)

	return nil
}
