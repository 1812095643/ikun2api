# IKUN2api 1.5.11

- 同步 Sub2API v0.2.23，加入 GPT-6.2 Sol 和 Astra，并合并流式超时切换和缓存写入计费修复。
- 支持渠道独立设置 Ultra Fast 倍率。账号设置优先，未配置时按渠道设置和模型默认价依次回退。已有下游计费范围继续生效。
- Claude OAuth 账号可在编辑页选择订阅档位，在账号列表显示标识。选择「自动」可恢复按现有账号资料显示。
- 更新会在 `channel_model_pricing` 增加可空的 `ultrafast_multiplier` 字段。已有渠道记录继续使用原倍率规则。
