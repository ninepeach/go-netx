# go-netx 测试清单（Test Checklist）

本文档有三个用途：

1. **go-netx 的测试矩阵**：每个检查点有固定 ID，标注当前状态，并对应到具体测试函数。
2. **Review 对照表**：审查代码或 PR 时，按 ID 核对测试是否覆盖，而不是凭感觉判断。
3. **网络库通用问题清单**：第 14 节总结了开发网络库时反复出现的问题类型，以后写其他网络库时可以直接拿来逐项检查。

本文档随代码更新，更新规则见末尾。测试由多方共同补充（包括 `test-matrix` 分支），合并时统一登记到这里。

## 0. 标记说明

**优先级**

- **P0**：核心正确性，必须覆盖。
- **P1**：重要边界、并发和异常路径。
- **P2**：资源上限、可观测性、平台差异和极端情况。

**状态**

| 标记 | 含义 |
|---|---|
| ✅ | 已覆盖，测试函数名列在“依据”一栏 |
| ⚠️ | 部分覆盖，或只被间接覆盖 |
| ❌ | 尚未覆盖，需要补充测试 |
| 🔧 | 已知代码问题，尚未修复（见第 12 节） |
| ➖ | 不适用：这个 API 有意不提供该行为，“依据”一栏写明原因 |
| ❓ | 设计待定，需要先确定 API 约定 |

“依据”一栏的测试名省略了 `Test` 前缀。不同包里的同名测试，用所在包名区分。

## 1. 通用完成标准

每个模块都要满足以下标准，才能算通过 Review：

1. 每个公开 API 至少有一个正常路径测试。
2. 每个输入参数都有边界测试，包括 0、负数、上限、上限加一。
3. 每个资源申请都有对应的释放路径测试，包括成功、失败和被拒绝三条路径。
4. 每个返回错误的分支都有测试。
5. 每个并发共享的状态都有 race 测试。
6. Close、Shutdown、ctx 取消都有测试，并且覆盖“正在等待时被关闭”的情况。
7. 重要状态机有状态转换测试。
8. 热路径有 Benchmark，并且断言分配次数。
9. 平台相关代码在对应平台上有测试，其他平台至少能交叉编译通过。
10. 所有已发现的回归项都有回归测试（第 11 节）。

## 2. socket：Socket 配置

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| SOCK-01 | P0 | 不设置任何选项时能正常 Listen | ✅ | ListenConfigWithoutOptions |
| SOCK-02 | P0 | RecvBuffer、SendBuffer 生效 | ✅ | ListenConfigAppliesBuffersBeforeBind（RecvBuffer 和 SendBuffer 都检查了，unix） |
| SOCK-03 | P0 | 缓冲区大小在 bind 之前设置（Control 阶段） | ✅ | ListenConfigAppliesBuffersBeforeBind（在 listener 上读取，证明是在 bind 前设置的） |
| SOCK-04 | P1 | accept 出来的 TCP 连接继承 listener 的缓冲区设置 | ✅ | AcceptedConnInheritsBuffers（Linux）、AcceptedConnInheritsBuffersDarwin |
| SOCK-05 | P1 | 缓冲区与 ReusePort、FastOpen 组合使用时都生效 | ❌ | |
| SOCK-06 | P1 | 不支持的平台返回 `UnsupportedOptionError`，`IsUnsupported` 能识别 | ✅ | FastOpenPolicy（所有非 Linux 平台）、UnsupportedOptionsAreExplicitOnDarwin |
| SOCK-07 | P1 | `UnsupportedIgnore` 只忽略“不支持”类错误，不吞掉其他系统错误 | 🔧 | REG-01：Linux 下 FastOpen 的任何 setsockopt 错误都会被忽略 |
| SOCK-08 | P1 | `UnsupportedError` 下错误能向上传递 | ✅ | FastOpenPolicy |
| SOCK-09 | P1 | `socket.Dialer` 返回副本，不修改调用方传入的 `net.Dialer` | ✅ | DialerCopiesBaseAndAppliesBuffers |
| SOCK-10 | P1 | 缓冲区为 0 或负数时不设置 | ❌ | |
| SOCK-11 | P2 | `SetBuffers` 作用于已存在的连接 | ✅ | SetBuffersOnExistingConnection |
| SOCK-12 | P2 | 各平台都能编译 | ✅ | 交叉编译（NF-17） |

## 3. tcp：TCP 传输

### 3.1 Listen 与 Accept

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| TCP-01 | P0 | Listen 成功并能接受连接 | ✅ | EchoAndStats |
| TCP-02 | P0 | 端口已被占用时返回错误 | ❌ | |
| TCP-03 | P0 | 多个客户端连续连接都能被接受 | ✅ | integration: TCPLineEcho |
| TCP-04 | P0 | 关闭后 Serve 返回，再次 Serve 返回 `ErrClosed` | ✅ | ServeContextCancelAborts |
| TCP-05 | P1 | 临时 accept 错误触发退避重试，不会终止 | ❌ | 需要能注入临时错误的 listener |
| TCP-06 | P1 | 永久 accept 错误会 abort 所有连接并返回错误 | ✅ | PermanentAcceptErrorAbortsHandlers |
| TCP-07 | P1 | 退避期间 Close/Shutdown 能立即唤醒 | 🔧 | REG-02：用的是 `time.Sleep`，最长要等 1 秒 |
| TCP-08 | P1 | 退避时间有上限（1 秒） | ❌ | |
| TCP-09 | P1 | 并发多次调用 Close/Shutdown 不会 panic | ❌ | |
| TCP-10 | P1 | `Serve` 被调用两次时返回错误 | ❌ | |
| TCP-11 | P1 | `NewServer` 传入 nil listener 或 nil handler 时返回错误 | ❌ | |

### 3.2 连接生命周期

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| TCP-12 | P0 | 每个连接调用一次 handler，并分配唯一 ID | ✅ | ConnIDsUnique |
| TCP-13 | P0 | handler 返回后连接被关闭，Stats 中 Active 归零 | ✅ | EchoAndStats |
| TCP-14 | P0 | Admit 拒绝时关闭连接并计数 | ✅ | AdmitRejects |
| TCP-15 | P1 | Admit 的许可在所有路径上成对释放 | ⚠️ | AdmitRejects 只覆盖正常路径；关闭期间刚 accept 的连接（track 失败）这条路径未测 |
| TCP-16 | P1 | Drain：停止 accept，现有 handler 可以继续运行，ctx 不会被取消 | ✅ | ShutdownDrainsThenReturns |
| TCP-17 | P1 | Drain 超时后 abort：ctx 被取消，连接被关闭 | ✅ | ShutdownAbortsOnTimeout |
| TCP-18 | P1 | `Draining()` 在 StopAccept 时关闭 | ✅ | ShutdownDrainsThenReturns |
| TCP-19 | P1 | 关闭期间 accept 进来的连接不会漏登记，也不会泄漏 | ❌ | 竞态窗口很小，需要注入点 |
| TCP-20 | P1 | Serve 的 ctx 被取消等价于 abort | ✅ | ServeContextCancelAborts |
| TCP-21 | P1 | handler 能读到 Serve ctx 中的值 | ❌ | |
| TCP-22 | P2 | 大量连接建立和断开后，goroutine 和 FD 数量不持续增长 | ❌ | 见 NF-18 |

### 3.3 Dialer

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| DIAL-01 | P0 | 正常连接 | ✅ | Dialer |
| DIAL-02 | P0 | 连接失败时返回错误，并触发 `OnDial` | ✅ | DialEventFailure |
| DIAL-03 | P0 | ctx 取消后拨号及时结束 | ❌ | |
| DIAL-04 | P0 | 超时设置生效 | ❌ | |
| DIAL-05 | P1 | 成功时 `DialEvent` 带有本地和远端地址 | ✅ | DialEventSuccess |
| DIAL-06 | P1 | socket 选项作用于拨出的连接 | ❌ | |
| DIAL-07 | P1 | 回调出错时的处理 | ➖ | `OnDial` 没有返回值，只做观测 |
| DIAL-08 | P1 | 并发拨号没有共享状态竞争 | ❌ | |

## 4. udp：UDP 传输

### 4.1 Listen 与接收

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| UDP-01 | P0 | Listen 后能正常收发 | ✅ | Echo |
| UDP-02 | P0 | 开启 ReusePort 且 Readers 大于 1 时，包会分散到不同读循环 | ✅ | ReusePortReaders（Linux） |
| UDP-03 | P0 | 收到的数据和来源地址正确，IPv4 映射地址已还原 | ⚠️ | Echo 只验证了数据；双栈回包做过手动验证，没有留下测试 |
| UDP-04 | P0 | 零长度数据报能正常交给 handler | ❌ | |
| UDP-05 | P0 | 恰好 `MaxPacket` 字节的包能收到 | ✅ | DefaultMaxDatagram |
| UDP-06 | P1 | 超过 `MaxPacket` 的包被丢弃并计入 Truncated（Linux 靠 MSG_TRUNC 标志，Windows 靠 WSAEMSGSIZE 错误） | ✅ | TruncatedDropped、DefaultMaxDatagram |
| UDP-07 | P1 | 多个读循环并发运行没有数据竞争 | ✅ | ReusePortReaders、ConcurrentWritesAndClose（-race） |
| UDP-08 | P1 | 读错误分类正确：暂时性、截断、致命 | ✅ | sys: ClassifyRead |
| UDP-09 | P1 | 暂时性错误按上限 1 秒退避重试 | ⚠️ | FatalReadErrorStopsServe 只验证不会停止，未验证退避时长 |
| UDP-10 | P1 | 退避期间调用 Close 能立即唤醒 | ✅ | CloseInterruptsBackoff |
| UDP-11 | P1 | 致命错误关闭整个 Conn，所有读循环退出，Serve 返回该错误 | ✅ | FatalReadErrorStopsServe |
| UDP-12 | P1 | 关闭、ctx 取消后所有读循环退出，再次 Serve 返回 `ErrClosed` | ✅ | ServeAfterCloseAndContextCancel |
| UDP-13 | P2 | `Readers` 为 0 时取默认值、为负数时拒绝；上限 | ⚠️ | ListenOptionBoundaries 覆盖了 0 和负数；上限交给调用方，见 D-01 |
| UDP-14 | P2 | `MaxPacket` 为 0 时取默认值、为负数时拒绝、超过 `MaxDatagram` 时拒绝 | ✅ | ListenOptionBoundaries（0 和负数）、MaxPacketBound（上限） |
| UDP-15 | P2 | 读循环的 handler 路径零分配 | ✅ | HandlerDoesNotAllocate |

### 4.2 发送与 Reply

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| UDP-16 | P0 | `WriteTo` 发送内容正确 | ✅ | ReplyValidatesPacket（经由回退路径） |
| UDP-17 | P0 | `Reply` 回包内容正确 | ✅ | Echo |
| UDP-18 | P0 | 超过 `MaxDatagram` 时返回 `ErrTooLarge`；较小的 `MaxPacket` 不限制发送 | ✅ | WriteTooLarge |
| UDP-19 | P1 | IPv4 下 65000 字节能发出（低于 IPv4 理论上限 65507） | ⚠️ | DefaultMaxDatagram 在回环上发送过，未单独断言 |
| UDP-20 | P1 | IPv6 下的发送边界 | ❌ | |
| UDP-21 | P1 | `Reply(nil)` 返回 `ErrNilPacket` | ✅ | ReplyValidatesPacket |
| UDP-22 | P1 | Reader 为负数或越界时回退到按端口选 socket，不 panic | ✅ | ReplyValidatesPacket |
| UDP-23 | P1 | 并发调用 WriteTo、Reply，同时 Close | ✅ | ConcurrentWritesAndClose |
| UDP-24 | P1 | 关闭后发送返回 `ErrClosed` | ⚠️ | ConcurrentWritesAndClose 允许返回该错误，但没有断言一定返回 |
| UDP-25 | P1 | 并发多次调用 Shutdown | ✅ | ConcurrentShutdown |

### 4.3 内核丢包计数

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| UDP-26 | P1 | Linux 上成功开启 SO_RXQ_OVFL | ✅ | KernelDropsEnabled |
| UDP-27 | P1 | 不支持时 `KernelDropsEnabled` 为 false，不会冒充“零丢包” | ✅ | KernelDropsEnabled（非 Linux） |
| UDP-28 | P1 | 32 位计数回绕后，累计值仍然正确 | ✅ | DropCounterWraps |
| UDP-29 | P1 | 共享同一 socket 的多个读循环上报的旧值不会被重复累加 | ✅ | DropCounterWraps、DropCounterWrapAndStaleReports |
| UDP-30 | P1 | cmsg 解析：能找到目标项，忽略无关项，拒绝截断数据和虚报长度 | ✅ | sys: DropCountParse |
| UDP-31 | P2 | 真实丢包场景下计数会增加 | ❌ | 需要压测：缓冲区设小，并让 handler 变慢 |

## 5. flow：Flow 表

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| FLOW-01 | P0 | ID 唯一、严格递增，0 不会被分配 | ✅ | IDsIncrease |
| FLOW-02 | P0 | `GetOrOpen` 不存在时创建，存在时复用 | ✅ | RemoveChecksEpoch |
| FLOW-03 | P0 | 状态机：Open → Closing → Closed，`BeginClose` 只在第一次调用时成功 | ✅ | CloseOnce |
| FLOW-04 | P0 | 并发 Close 时只有一个调用者拿到 true | ✅ | CloseOnce |
| FLOW-05 | P1 | `Remove` 校验 ID（epoch），旧 ID 删不掉复用了同一 key 的新 flow | ✅ | RemoveChecksEpoch |
| FLOW-06 | P1 | 无论通过 Remove、Sweep 还是 Close 离开表，`OnClose` 都恰好调用一次 | ✅ | ConcurrentRemovalClosesFlowOnce（Remove、Sweep、Close 三者并发） |
| FLOW-07 | P1 | 同一个 key 并发 `GetOrOpen` 只创建一个 flow | ✅ | GetOrOpenConcurrentSingleCreation |
| FLOW-08 | P1 | 闲置的 flow 会被清理 | ✅ | SweepIdle、ActiveFlowSurvivesSweep |
| FLOW-09 | P1 | 持续被访问的 flow 在 Run 并发清扫时不会被清理 | ✅ | ActiveFlowSurvivesSweep |
| FLOW-10 | P1 | 不运行 Run、只手动 Sweep 时也会推进时钟 | ✅ | ManualSweepKeepsTouchedFlow |
| FLOW-11 | P1 | 传入更早的时间时时钟不会倒退 | ✅ | SweepClockNeverMovesBackward |
| FLOW-12 | P1 | 多个 Sweep 并发执行时不会重复调用 `OnClose` | ⚠️ | ConcurrentRemovalClosesFlowOnce 覆盖了 Sweep 与其他关闭路径并发，多个 Sweep 之间的并发未测 |
| FLOW-13 | P1 | Run 退出时关闭所有 flow | ✅ | RunClosesAllOnExit |
| FLOW-14 | P2 | 大量 flow 下 Sweep 的耗时（文档写的是每百万约 20ms） | ❌ | 需要 Benchmark |
| FLOW-15 | P2 | 不同 key 哈希冲突时不会被合并 | ➖ | 用的是 Go map，key 按值比较，哈希只用来选分片 |
| FLOW-16 | P2 | 查找零分配 | ✅ | BenchmarkGetAddrPort |

## 6. limit：资源控制

### 6.1 Concurrency

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| RES-01 | P0 | 未达上限时允许，达到上限时拒绝，释放后恢复 | ✅ | Concurrency |
| RES-02 | P0 | 并发情况下持有者数量不超过上限 | ✅ | ConcurrencyParallel |
| RES-03 | P0 | `InUse` 在任何时刻都不超过 `Max` | ✅ | ConcurrencyNeverExceedsMax |
| RES-04 | P1 | Release 次数多于 Acquire 时不会多出额度 | 🔧 | REG-13：计数会变成负数，相当于凭空多出额度；Budget 在这种情况下会 panic，两者行为不一致 |
| RES-05 | P1 | `max <= 0` 时 panic | ❌ | |
| RES-06 | P1 | 等待、ctx 取消、超时 | ➖ | `TryAcquire` 从不阻塞，这是设计上的选择 |

### 6.2 Rate 和 All

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| RES-07 | P0 | 令牌桶的突发上限和补充速率正确 | ✅ | Rate |
| RES-08 | P1 | `All` 部分成功时会释放已经拿到的许可 | ✅ | AllReleasesOnPartialFailure |
| RES-09 | P1 | 参数非法时 panic | ❌ | |

### 6.3 Budget

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| RES-10 | P0 | 预留、拒绝、释放的数值正确 | ✅ | Budget |
| RES-11 | P1 | 负数预留被拒绝 | ✅ | BudgetRefusesNegative |
| RES-12 | P1 | 释放负数或超过已预留的量时 panic | ✅ | BudgetRefusesNegative |
| RES-13 | P1 | 已有占用时预留 `MaxInt64` 不会溢出，恰好填满时成功 | ✅ | BudgetOverflow |
| RES-14 | P1 | 并发预留时 `Used` 不超过 `Max` | ✅ | BudgetParallelNeverExceeds |
| RES-15 | P1 | `max <= 0` 时 panic | ❌ | |

## 7. queue：有界队列

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| QUEUE-01 | P0 | 先进先出，顺序正确 | ⚠️ | DropOldestCallsOnDrop 间接验证 |
| QUEUE-02 | P0 | 容量向上取整为 2 的幂 | ✅ | CapRoundsUp |
| QUEUE-03 | P0 | Reject：满时拒绝，所有权仍归调用方 | ✅ | RejectKeepsOwnership |
| QUEUE-04 | P0 | DropOldest：挤掉最旧的元素并交给 `OnDrop` | ✅ | DropOldestCallsOnDrop |
| QUEUE-05 | P1 | 容量为 0 或负数时 panic | ❌ | |
| QUEUE-06 | P1 | 关闭后拒绝新元素，已有元素仍能取出，之后返回 `ErrClosed` | ✅ | CloseDrainsThenErrClosed |
| QUEUE-07 | P1 | 重复 Close 是安全的 | ⚠️ | CloseRacingPushPreservesBound 覆盖了 Close 与 Push 并发，重复 Close 未测 |
| QUEUE-08 | P1 | Close 唤醒所有正在等待的消费者 | ✅ | CloseWakesWaiters |
| QUEUE-09 | P1 | Close 唤醒正在等待的生产者（`PushWait`） | ✅ | CloseWakesBlockedProducer |
| QUEUE-10 | P1 | `PushWait` 在有空位时被唤醒 | ✅ | PushWaitUnblocksOnPop |
| QUEUE-11 | P1 | `PushWait` 和 `PopWait` 能被 ctx 取消 | ⚠️ | PushWaitContext 只测了 PushWait |
| QUEUE-12 | P1 | Drain 把剩余元素逐个交给 `OnDrop` | ✅ | DrainReleases |
| QUEUE-13 | P1 | 多生产者、单消费者：不丢失、不重复 | ✅ | ConcurrentNoLoss |
| QUEUE-14 | P1 | 多消费者：不丢失、不重复，也不会丢失唤醒信号 | ❌ | |
| QUEUE-15 | P2 | Stats 的各项计数与实际操作一致 | ⚠️ | 只检查了 Rejected 和 Len |
| QUEUE-16 | P2 | Push 和 Pop 零分配 | ✅ | BenchmarkPushPop |
| QUEUE-17 | P1 | Close 与并发 Push 同时发生时，长度不超过容量，关闭后拒绝新元素 | ✅ | CloseRacingPushPreservesBound |

## 8. buf：缓冲区

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| BUF-01 | P0 | 按大小选到正确的档位；超大请求不进池 | ✅ | GetClasses |
| BUF-02 | P0 | `len(B)` 等于请求的大小 | ✅ | GetClasses |
| BUF-03 | P0 | `Copy` 不与源数据共享内存 | ✅ | Copy |
| BUF-04 | P0 | 预算按容量扣除，释放后归还，超出时 Get 返回 nil | ✅ | BudgetChargesCapacity、BudgetRejectsLargeBufferWithoutPartialCharge、ReleaseClearsViewAndRefundsUnpooledCapacity |
| BUF-05 | P1 | 在 `netxdebug` 下重复 Release 会 panic | ✅ | DoubleReleasePanicsInDebug |
| BUF-06 | P1 | 在 `netxdebug` 下 `Outstanding` 能反映泄漏 | ✅ | integration: UDPGatewayComposition |
| BUF-07 | P1 | 释放后继续使用的行为 | ➖ | 约定为调用方的 bug，正式构建中无法检测 |
| BUF-08 | P1 | 并发 Get 不会返回同一块正在使用的内存 | ❌ | |
| BUF-09 | P1 | 负数大小时 panic | ✅ | GetNegativeSizePanics |
| BUF-10 | P1 | 扩容、Reset | ➖ | 没有这两个 API，Buf 的大小固定 |
| BUF-11 | P2 | GC 回收池中对象后，正确性不受影响 | ⚠️ | sync.Pool 的语义保证了这一点，没有专门测试 |
| BUF-12 | P2 | 热路径零分配 | ✅ | BenchmarkGetRelease |
| BUF-13 | P1 | Release 后 `B` 被置为 nil，避免继续使用旧的切片 | ✅ | ReleaseClearsViewAndRefundsUnpooledCapacity |
| BUF-14 | P1 | 预算不足时不会先扣掉一部分 | ✅ | BudgetRejectsLargeBufferWithoutPartialCharge |

## 9. internal 与可观测性

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| SYS-01 | P1 | `ClassifyRead` 对各类 errno 的分类正确 | ✅ | ClassifyRead（unix） |
| SYS-02 | P1 | Windows 上的错误分类 | ❌ | 需要在 Windows 上运行 |
| SYS-03 | P1 | `DropCount` 解析正确且零分配 | ⚠️ | DropCountParse 验证了正确性，未断言分配次数 |
| SYS-04 | P1 | `counter.Max` 在并发下单调不减 | ✅ | counter: CounterMaxConcurrent、CounterOperations |
| OBS-01 | P0 | TCP Stats：Accepted、Active、Closed、Rejected | ✅ | EchoAndStats、AdmitRejects |
| OBS-02 | P1 | TCP Stats：AcceptErrors | ❌ | 依赖 TCP-05 |
| OBS-03 | P0 | UDP Stats：收发的包数和字节数 | ✅ | Echo |
| OBS-04 | P1 | UDP Stats：Truncated、RecvErrors | ✅ | TruncatedDropped、FatalReadErrorStopsServe |
| OBS-05 | P1 | UDP Stats：SendErrors | ❌ | |
| OBS-06 | P1 | 读取 Stats 与更新同时进行时没有数据竞争 | ⚠️ | 有部分测试在 -race 下并发读取 Stats |
| OBS-07 | P2 | 关闭指标采集时不影响功能 | ➖ | 没有开关，计数器始终开启，只是一次原子加法 |

## 10. Runtime：生命周期

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| RUN-01 | P0 | 执行顺序：OnStart → Serve → Shutdown → OnStop | ✅ | OrderAndShutdown |
| RUN-02 | P0 | Server 只能运行一次，第二次返回 `ErrAlreadyRunning` | ✅ | OneShotAndPanic |
| RUN-03 | P0 | 父 ctx 取消时走 drain 流程，而不是直接 abort | ✅ | ParentCancelDrainsNotAborts |
| RUN-04 | P0 | Serve 返回的错误会结束运行并被返回 | ✅ | ServeErrorStopsAndIsReturned |
| RUN-05 | P1 | OnStart 失败时调用 Service.Shutdown 回收资源 | ✅ | StartErrorShutsDown |
| RUN-06 | P1 | hook 中的 panic 被转换成错误 | ✅ | OneShotAndPanic |
| RUN-07 | P1 | 服务卡住时最多等待 `AbortGrace` | ✅ | StuckServiceBoundedByAbortGrace |
| RUN-08 | P1 | OnStop 有独立的 ShutdownTimeout 预算 | ✅ | OnStopGetsFreshContext、OnStopHasIndependentTimeoutBudget |
| RUN-09 | P1 | drain 期间收到第二次信号会立即 abort | ✅ | SecondSignalAborts（unix） |
| RUN-10 | P1 | 在运行前或停止后调用 Stop/Shutdown，返回 `ErrNotRunning` | ❌ | |
| RUN-11 | P1 | 并发调用 Stop 和 Shutdown | ❌ | |
| RUN-12 | P1 | OnStop 返回的错误会被合并进最终结果 | ✅ | OnStopErrorIsReturned |
| RUN-13 | P1 | `Group`：一个服务出错，其余服务被 abort | ❌ | |
| RUN-14 | P1 | `Group`：Shutdown 并发进行，错误合并返回 | ⚠️ | GroupWithRealTransports 只测了正常路径 |
| RUN-15 | P2 | 连续收到多次信号不会死锁 | ❌ | |
| RUN-16 | P2 | 停止后后台 goroutine 全部退出 | ❌ | 见 NF-18 |

## 11. 集成测试

| ID | P | 检查点 | 状态 | 依据 |
|---|---|---|---|---|
| INT-01 | P0 | Runtime 管理 TCP 服务，多个客户端同时通信 | ✅ | TCPLineEcho |
| INT-02 | P0 | Runtime 通过 Group 同时管理 TCP 和 UDP | ✅ | GroupWithRealTransports |
| INT-03 | P0 | UDP + Flow + Buf + Queue + Limit 按 Gateway 的方式组合 | ✅ | UDPGatewayComposition |
| INT-04 | P1 | 在 `netxdebug` 下，整条链路零缓冲区泄漏 | ✅ | UDPGatewayComposition |
| INT-05 | P1 | TCP + Flow：连接关闭时清理对应的 flow | ❌ | |
| INT-06 | P1 | 队列满时 DropOldest 释放缓冲区，不泄漏 | ⚠️ | 有接入，但压力不够大，没有真正把队列打满 |
| INT-07 | P1 | Budget 耗尽时丢包，恢复后继续正常工作 | ❌ | |
| INT-08 | P1 | TCP 在 drain 期间，长连接收到 `Draining` 信号后主动退出 | ⚠️ | 只有单元测试 |
| INT-09 | P2 | 长时间运行，FD、内存、goroutine 都不泄漏 | ❌ | 见 NF-18 |

## 12. 回归清单与已知问题

Review 中发现的问题都要登记在这里，并对应到一个测试 ID。修复后状态改为 ✅，不要删除这一行。

| ID | 模块 | 问题 | 测试 | 状态 |
|---|---|---|---|---|
| REG-01 | socket | `UnsupportedIgnore` 会吞掉所有 FastOpen setsockopt 错误，不只是“不支持”类错误 | SOCK-07 | 🔧 未修复 |
| REG-02 | tcp | accept 的退避用 `time.Sleep`，Close 时无法被唤醒 | TCP-07 | 🔧 未修复 |
| REG-03 | udp | 读错误退避无法被 Close 唤醒 | UDP-10 | ✅ |
| REG-04 | udp | `Reply(nil)` 会 panic | UDP-21 | ✅ |
| REG-05 | udp | Reply 时 Reader 越界会 panic | UDP-22 | ✅ |
| REG-06 | udp | 开启丢包计数的结果被忽略，无法区分“零丢包”和“没统计” | UDP-26、27 | ✅ |
| REG-07 | udp | `Readers` 没有上限 | UDP-13 | ❓ 见 D-01 |
| REG-08 | udp | `MaxPacket` 没有上限 | UDP-14 | ✅ 上限为 `MaxDatagram`（65000） |
| REG-09 | runtime | drain 耗尽时间后，OnStop 拿到的 ctx 已经过期 | RUN-08 | ✅ |
| REG-10 | runtime | 第二次信号被忽略 | RUN-09 | ✅ |
| REG-11 | flow | 只手动 Sweep 时会误清理活跃的 flow | FLOW-10 | ✅ |
| REG-12 | limit | Budget 在接近 `MaxInt64` 时溢出 | RES-13 | ✅ |
| REG-13 | limit | `Concurrency.Release` 多调用时，计数变负，凭空多出额度 | RES-04 | 🔧 未修复 |
| REG-14 | limit | Budget 接受负数预留 | RES-11 | ✅ |
| REG-15 | flow | Get 与 Sweep 竞态：取出 flow 和刷新时间之间被清理 | FLOW-09 | ✅ |
| REG-16 | udp | 永久读错误会无限重试 | UDP-11 | ✅ |
| REG-17 | tcp | 永久 accept 错误时，等待长连接 handler 导致无法退出 | TCP-06 | ✅ |
| REG-18 | udp | `KernelDrops` 在 32 位回绕后停止增长 | UDP-28 | ✅ |

### 设计决定

这里记录“看起来像问题，但经过讨论有意这样设计”的项，避免以后重复讨论。

| ID | 决定 | 理由 |
|---|---|---|
| D-01 | 不限制 `Readers` 的上限 | 规模决策交给调用方，文档建议不超过 CPU 核数 |
| D-02 | 收发统一使用 `MaxDatagram = 65000` | 收发规则一致；超过 65000 的包极少见，代价是这类包会被计入 Truncated |
| D-03 | UDP handler 没有 ctx，也不能被中断 | 换取零分配、无额外 goroutine；需要等待的工作交给 Gateway 的队列 |
| D-04 | `Concurrency.TryAcquire` 不阻塞 | Admission 只回答“现在能不能进”，排队属于 queue 的职责 |
| D-05 | Buf 的预算只统计正在使用的内存 | sync.Pool 中闲置的内存会被 GC 回收，无法也不需要计入 |

## 13. 非功能测试

| ID | 类型 | 检查点 | 状态 | 命令或说明 |
|---|---|---|---|---|
| NF-01 | Race | 所有包 | ✅ | `go test -race ./...` |
| NF-02 | Race | Debug 构建 | ✅ | `go test -race -tags netxdebug ./...` |
| NF-03 | Stress | TCP 大量并发连接 | ❌ | |
| NF-04 | Stress | UDP 高并发读写，并真正触发内核丢包 | ❌ | |
| NF-05 | Stress | Flow 并发创建与清理 | ⚠️ | ActiveFlowSurvivesSweep、GetOrOpenConcurrentSingleCreation、ConcurrentRemovalClosesFlowOnce |
| NF-06 | Stress | Queue 多生产者、多消费者 | ⚠️ | 只有 ConcurrentNoLoss（单消费者） |
| NF-07 | Fuzz | `DropCount` 解析任意字节不会 panic | ❌ | 最适合 fuzz 的地方 |
| NF-08 | Fuzz | UDP Packet 参数 | ❌ | |
| NF-09 | Fuzz | Flow 状态机的随机操作序列 | ❌ | |
| NF-10 | Fuzz | Queue 的随机操作序列，检查不变量 | ❌ | |
| NF-11 | Fuzz | Buf 的随机 Get/Release 序列 | ❌ | |
| NF-12 | Bench | UDP 回显 | ✅ | BenchmarkEcho（约 10µs，0 次分配） |
| NF-13 | Bench | TCP accept 吞吐 | ❌ | |
| NF-14 | Bench | 不同 flow 数量下的查找和清扫 | ⚠️ | 只有单个 key 的查找 |
| NF-15 | Bench | Queue | ✅ | BenchmarkPushPop |
| NF-16 | Bench | Buf | ✅ | BenchmarkGetRelease |
| NF-19 | Bench | 共享计数器 | ✅ | BenchmarkCounterInc |
| NF-17 | 交叉编译 | linux/amd64、linux/386、darwin/arm64、windows/amd64、freebsd/amd64、js/wasm | ✅ | `GOOS=… GOARCH=… go vet ./...` |
| NF-18 | 泄漏 | 生命周期结束后 goroutine 和 FD 被回收 | ❌ | 可以用 goleak，或在测试前后比较 `runtime.NumGoroutine` |

## 14. 网络库通用问题清单

这一节和 go-netx 的具体 API 无关，总结的是开发网络库时反复出现的问题类型。写新的网络库，或者审查别人的网络库时，可以逐项检查。括号里是 go-netx 中对应的实际案例。

### 14.1 等待与关闭

- [ ] 所有退避、重试的等待都能被关闭唤醒，不能用不可中断的 `time.Sleep`（REG-02、REG-03）。
- [ ] 关闭操作能唤醒所有等待者：消费者、生产者，以及阻塞在 I/O 上的 goroutine（QUEUE-08、QUEUE-09）。
- [ ] “停止接收新请求”和“取消正在处理的请求”是两个独立的阶段，handler 的 ctx 只在 abort 时取消（TCP-16）。
- [ ] 每个关闭阶段都有独立的超时预算，后一阶段不会继承前一阶段已经过期的 ctx（REG-09）。
- [ ] 卡住的组件不会让整个进程无法退出：有兜底超时，第二次信号能强制退出（RUN-07、REG-10）。
- [ ] 永久性错误会主动终止并向上报告，不会无限重试，也不会让服务处于“看起来正常、实际已经不可用”的状态（REG-16、REG-17）。

### 14.2 资源上限的算术

- [ ] 拒绝负数输入（REG-14）。
- [ ] 做加法之前先检查是否会溢出，不要“先加上去，超了再减回来”（REG-12）。
- [ ] 失败的尝试不会让计数短暂超过上限，要用 CAS 而不是乐观地加减（RES-03）。
- [ ] 释放次数多于申请次数时要报错，不能悄悄多出额度（REG-13）。
- [ ] 内核提供的 32 位计数器会回绕，要按增量累加；多个读者乱序上报的旧值不能重复累加（REG-18）。

### 14.3 所有权与泄漏

- [ ] 每块资源在任何时刻都只有一个主人，借用、持有、转移三种状态都写进文档。
- [ ] 转移失败时，所有权仍归原主人（QUEUE-03）。
- [ ] 丢弃路径（队列溢出、关闭时剩余的元素、错误分支）都会释放资源，这是最容易泄漏的地方（QUEUE-04、QUEUE-12）。
- [ ] 提供调试构建，能检测重复释放并统计未释放的数量（BUF-05、BUF-06）。
- [ ] 传给回调的数据是借用的，回调返回后就失效；要保留必须先复制。

### 14.4 并发与竞态

- [ ] “查找”和“更新状态”要在同一把锁内完成，否则清理任务可能插在两者之间（REG-15）。
- [ ] 后台任务推进的共享时钟，在手动调用时也要推进（REG-11）。
- [ ] 关闭和新请求之间的竞态窗口：刚 accept 进来的连接，在关闭时也要被登记或释放（TCP-19）。
- [ ] 回调不要在持锁时调用，或者在文档中明确写出持锁回调的约束。
- [ ] 共享 socket 的多个读者在更新同一份统计时，要考虑乱序。

### 14.5 输入与 API 边界

- [ ] 公开类型的字段可能被调用方伪造，公开方法遇到非法值时返回错误，不要 panic（REG-04、REG-05）。
- [ ] 配置项都要确定 0、负数和上限各自的含义，并写进文档（UDP-13、UDP-14）。
- [ ] 没有意义的配置值直接拒绝，比如超过协议上限的缓冲区大小。
- [ ] 能力检测的结果要对外暴露，不能让“不支持”看起来像“零”（REG-06）。
- [ ] “忽略不支持”的策略只能忽略不支持类的错误，不能把其他错误也一起吞掉（REG-01）。

### 14.6 平台差异

- [ ] 同一种语义在不同平台上可能以不同方式出现，比如截断：Linux 设置 MSG_TRUNC 标志，Windows 返回错误（UDP-06）。
- [ ] ICMP 错误在 Windows 上会以读错误的形式出现在未连接的 UDP socket 上，要归为暂时性错误。
- [ ] socket 选项要在 bind 或 listen 之前设置，才能被 accept 出来的连接继承，TCP 窗口缩放也才能生效（SOCK-03、SOCK-04）。
- [ ] 每个声明支持的平台都要交叉编译通过；只在某个平台上运行的测试，用构建标签隔离（NF-17）。

### 14.7 性能

- [ ] 热路径不创建 goroutine、不分配内存、不经过内部队列，并用 Benchmark 断言分配次数。
- [ ] 按核数分配的内存可以预先分配，按连接数分配的内存要用池，绝不按包分配。
- [ ] 共享计数器按 cache line 对齐，或者按读者分片，避免伪共享。
- [ ] 全表扫描的成本随规模线性增长，要在文档中写明，并用 Benchmark 记录。

## 15. 测试执行分层

| 层次 | 命令 | 何时执行 |
|---|---|---|
| 单元测试 | `go test ./...` | 每次提交 |
| Race | `make race`（包含 `-race` 和 `-race -tags netxdebug` 两轮） | 每个 PR |
| Benchmark | `make bench` | 涉及性能的改动前后各跑一次，对比结果 |
| 交叉编译 | 对 NF-17 列出的每个平台执行 `GOOS=… GOARCH=… go vet ./...` | 每个 PR 和每次发布 |
| Fuzz | `go test -fuzz=<函数名> -fuzztime=30s ./<包>` | 定期执行；fuzz 要按包和函数名分别运行 |
| Stress / 泄漏 | 尚未建立（NF-03～06、NF-18） | 合并前，或每晚定时执行 |

## 16. 更新规则

1. **新增公开 API**：同一个 PR 里要在对应模块下加上检查点，状态不能是空白。
2. **Review 发现问题**：在第 12 节登记一个 REG 编号，并对应到一个检查点；修复后状态改为 ✅，不要删除这一行。
3. **讨论后决定不修改**：登记到“设计决定”，写明理由。
4. **新增测试**：把对应检查点的状态改为 ✅ 或 ⚠️，并在“依据”一栏写上测试函数名。
5. **ID 不复用**：检查点删除后，它的 ID 也不再分配给其他检查点。
6. **第 14 节只放通用经验**：只和 go-netx 有关的内容放在前面各模块的章节里。
