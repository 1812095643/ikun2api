# 真实数据回放

`export-replay.py` 从已授权服务器直接运行仓库迁移文件中4个证据视图的SELECT；外层只读事务，不安装视图、不读凭据列、不改业务表。必须提供固定24小时截止时间、已核验known_hosts及已有SSH身份。输出0600并拒绝覆盖；含内部ID，保存在Git忽略的私有目录，不提交原始证据。

```sh
python3 deploy/ops-monitor/tests/export-replay.py \
  --host root@YOUR_SERVER --known-hosts /absolute/path/pinned_known_hosts \
  --until 2026-11-13T21:41:42Z --output /private/path/evidence.jsonl
```

当前回归数据集固定为2026-11-12T21:41:42Z至2026-11-13T21:41:42Z（北京时间11月13日05:41:42至14日05:41:42），预期666个错误、373个尝试、7895个用量、8个系统事件。后续换窗口需要同时调整测试窗口与源统计断言，不把新数据的变化当回归失败。

backend目录执行：

```sh
TEST_MONITOR_REPLAY_JSON=/private/path/evidence.jsonl \
TEST_MONITOR_REPLAY_REPORT=/private/path/report.json \
go test ./internal/monitoring/runtime -run TestReal -v -count=1
```

可以设置TEST_MONITOR_REPLAY_DSN连接只包含这些投影的隔离PostgreSQL合同视图；此时直接使用正式Postgres.Poll的只读事务、分页游标及Collector/Evaluator/Manager/SQLite/Worker。未设置时使用归档回放采集器，报告postgres_adapter=false，不能混同。两种模式均按原时序逐分钟运行；使用真实Feishu协议适配器，测试transport仅将固定官方origin定向到本地HTTP接收端。没有自定义生产通知URL设置，也没有真实飞书凭据。模拟429、恢复成功样本与真实历史分开计数。

覆盖：所有错误逐条分类、所有保留尝试、缺失task_id的计费错误、同类事件合并、同message_id更新、429后重试、重启重放无重复、新装历史隔离。报告区分新卡片和更新次数；更新不是新建消息，也不代表用户已读。

这不等于生产Postgres采集器已部署或真实飞书收到消息。常规只读角色/分页/迁移合同测试仍在sources包，必须只对一次性隔离数据库设置TEST_MONITOR_PG_DSN，不能指向生产。
