# internal/wallet

积分余额 + 流水。金额一律**整数 microunit**（1 积分 = 1_000_000），不用浮点。

**主要类型**：`Store` · `Move`（一次记账的描述）· `Entry`（一条流水）· `Balance`。

**两条不变量，双保险**：
1. **balance 变更和 ledger 插入同事务** —— 否则会出现「钱扣了但流水没记」，对账说不清
2. **balance / reserved 永不为负** —— 条件 UPDATE（`WHERE balance >= ?`）第一层，表上 CHECK 第二层。
   并发靠 `BEGIN IMMEDIATE` 串行化（见 `internal/db`）

**Amount 恒为正数**，方向由 `Debit` / `Credit` 决定 —— 避免调用方传错符号。
流水里存的是带符号金额（出账为负）。

**冻结三步**（拉号用 · `09-transactions §2`）：
- `Reserve` balance → reserved · **不记流水**（钱还没花掉）
- `CommitReserved` reserved 减 + 记流水（真花掉）
- `ReleaseReserved` reserved → balance（失败或成交价低于预估）

**`*Tx` 版本**（`ApplyTx` / `ReserveTx` / …）能挂进调用方的事务 ——
拉号那种「扣钱 + 建 pull_round + 建 credential_ledger」必须整体原子。
