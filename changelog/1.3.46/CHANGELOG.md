# IKUN2api 1.3.46

- 修复独立号池分享页在生产内嵌前端模式下未应用专用响应头的问题。实际 HTML 返回路径统一设置 `Cache-Control: no-store`、`Referrer-Policy: no-referrer`、`X-Robots-Tag: noindex, noarchive`。
- 分享页不使用 ETag/304，覆盖缓存命中和旧版回退路径；其他页面继续使用原缓存策略。
- 增加使用实际 embed 构建的响应头回归测试。
- 包含 1.3.45 的用户号池百分比与并发展示、管理员多选分组、只读分享、一次确认批量导入和资料库两步验证开关；无新增迁移。
