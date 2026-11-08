# IKUN2api 1.4.6

- 用户端号池只展示 OAuth 类型账号，api_key、bedrock、service_account 等类型不再出现在用户列表里。
- 用户端号池不再受「可见分组」限制：所有分组下的 OAuth 账号都会列出，账号所属分组照旧展示；使用记录与今日用量仍只统计当前用户本人。
- 合成虚拟账号的邮箱域名统一为 outlook.com，不再混入 hotmail、live、outlook.jp 等一眼可辨的随机域名。
