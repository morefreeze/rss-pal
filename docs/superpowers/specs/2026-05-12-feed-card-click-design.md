# 订阅卡片点击跳转文章列表

## 背景

`/feeds` 页面（`FeedListPage`）目前以卡片形式展示订阅列表。每张卡片右侧有「刷新 / 暂停 / 删除」按钮，但卡片本体（feed 名称、URL、文章数等区域）没有点击交互。

`/articles` 页面（`ArticleListPage`）已支持按 feed 筛选：顶部的下拉框写入 `sessionStorage.selectedFeed`，组件在 mount 时读取该值作为初始筛选条件。

本设计在两端之间补一座桥：在 `/feeds` 上点击订阅卡片，直接跳到 `/articles` 并只显示该 feed 下的文章。

## 设计

### 交互

- 每张订阅卡片整体可点（hover 时显示浅色背景 + cursor pointer）。
- 点击 → `sessionStorage.setItem('selectedFeed', JSON.stringify(feed.id))` → `navigate('/articles')`。
- 右侧的「刷新 / 暂停 / 删除」按钮加 `e.stopPropagation()`，避免冒泡到卡片触发跳转。
- 卡片上方的「健康度面板 →」链接不在卡片内，不受影响。
- 已暂停（`is_active=false`）的卡片同样可点击 —— 历史文章仍然存在，用户也可能想回看。

### 视觉

- 卡片外层加上：`role="button"`、`tabIndex={0}`、`cursor: pointer`、`onMouseEnter/Leave` 切换 `background: var(--surface-hover)`（或直接用 CSS `:hover`）。
- 不改卡片内部的布局、字号、按钮样式。
- 键盘可达：`Enter` / `Space` 触发跳转（通过 `onKeyDown` 处理）。

### `/articles` 端的行为

- 不改 `ArticleListPage`。它的 `useState` 初始化逻辑已经从 `sessionStorage.selectedFeed` 读取初值，下拉框会自动显示当前选中的 feed。
- 其他筛选状态（`unreadOnly` / `savedOnly` / `grouped`）继续从 `sessionStorage` 读取 —— 只切换 feed，不重置其他条件。这和现有下拉框的行为一致。

## 不做的事（YAGNI）

- 不引入 query string（如 `/articles?feed_id=N`）。保持和现有下拉框相同的 `sessionStorage` 机制，避免两条路径。
- 不修改卡片内部按钮的样式、位置或行为。
- 不在卡片上加额外的"查看文章 →"提示文字 —— hover 背景 + cursor 已足够暗示可点。
- 不为不同 feed 类型（rss / html / saved）做特殊处理。点击行为统一。

## 影响范围

- 仅修改 `frontend/src/pages/FeedListPage.tsx`，约 10~20 行的范围（卡片包一层 div、三个 action 按钮加 `stopPropagation`）。
- 无后端、无 API、无数据库变更。
- 由于前端走 Docker，验证时需要 `docker-compose up -d --build frontend`。

## 验证

- 点击订阅卡片 → 跳到 `/articles`，下拉框显示该 feed，列表只含该 feed 的文章。
- 点击卡片上的「刷新 / 暂停 / 删除」按钮 → 仅触发对应操作，不跳转。
- 用键盘 Tab 聚焦到卡片，按 Enter → 等同于点击。
- 暂停的 feed 卡片同样可点，跳转后能看到历史文章。
- 现有的下拉框、`unreadOnly` / `savedOnly` / 分组视图等行为不变。
