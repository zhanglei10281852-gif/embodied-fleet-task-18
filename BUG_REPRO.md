# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

走廊预约到达 effective_at 后，数据库状态已经从 allocated 变成 effective，调度引擎的 WindowsActivated 却没有增加。本次只做诊断，不改代码，沿批量激活的返回值追到引擎实际接收的数字并给出两者不一致的证据；生产代码维持现状，激活测试继续使用原断言，时钟配置同样保持不变。

## 含 Bug 版本

- 仓库：zhanglei10281852-gif/embodied-fleet-task-18
- 仓库地址：https://github.com/zhanglei10281852-gif/embodied-fleet-task-18.git
- parent SHA：b27c3ef800006f35752c8a1cdee85267cc358961

## 复现步骤

```bash
git clone -- https://github.com/zhanglei10281852-gif/embodied-fleet-task-18.git bug-repro
cd bug-repro
git checkout --detach b27c3ef800006f35752c8a1cdee85267cc358961
go test ./internal/engine -run ^TestEngine_ActivateWindow$ -count=1
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/engine -run ^TestEngine_ActivateWindow$ -count=1
--- FAIL: TestEngine_ActivateWindow (0.21s)
    engine_test.go:207: expected at least 1 activation
FAIL
FAIL	github.com/zhanglei10281852-gif/embodied-fleet-go/internal/engine	0.220s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/engine -run ^TestEngine_ActivateWindow$ -count=1
--- FAIL: TestEngine_ActivateWindow (0.45s)
    engine_test.go:207: expected at least 1 activation
FAIL
FAIL	github.com/zhanglei10281852-gif/embodied-fleet-go/internal/engine	0.665s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

诊断应命中 internal/routing/service.go 的 Service.ActivateEffective 和 internal/engine/engine.go 的 Engine.tick，解释成功激活数被清零返回并以 observed=0 汇总的完整链路。证据需逐项对照数据库状态已为 effective、服务实际激活结果和 WindowsActivated 仍为零，目标仓库代码、测试和配置零改动。
