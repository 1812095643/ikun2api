# IKUN2api 1.5.9

- Anthropic 分组可开启「仅允许 IKUN Bridge」，默认关闭。Bridge 使用现有 API Key 领取短期通行证；直接访问受限分组时在调度前返回 `403`。
- 修复注册页未同意条款时点击输入框或「继续」没有反馈的问题。点击后在勾选框旁弹出提示，勾选后恢复使用。
- 更新会添加 `groups.bridge_required`，已有分组保持现有访问方式。
