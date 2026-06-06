# LDAP DN 规范化迁移说明

> **适用范围**:启用了 LDAP/AD 身份(`OTTERIO_IDENTITY_LDAP_SERVER_ADDR` 非空)
> 的 OtterIO 集群,且从未做过 DN 规范化的早期版本。

## 1. 背景

历史上,OtterIO 把 LDAP 服务器返回的 Distinguished Name (DN) 原样作为
IAM 策略表的 key:磁盘上是 `<DN>.json` 文件,内存里是 `iamUserPolicyMap[DN]`
和 `iamGroupPolicyMap[DN]`。所有比较都是字面量字符串相等。

但 RFC 4514 / RFC 4517 把 DN 定义为大小写不敏感、对 RDN 内部空格容忍、对
multi-valued RDN 顺序无关。Active Directory 默认就是这种语义。这意味着
同一个 LDAP 身份可能被表示成:

```
CN=Alice,OU=Users,DC=Corp,DC=Com
cn=alice,ou=users,dc=corp,dc=com
cn=Alice,  ou=users,dc=corp,dc=com
```

历史版本的 OtterIO 会把它们视为三个不同的 IAM 主体,从而:

- 同一个用户登录两次拿到不同的策略集;
- 攻击者(或不经意的运维)只要让目录返回另一种大小写,就能绕过原本设
  定的拒绝/允许策略;
- 同一个用户/组的策略可能被分散写到多个文件,管理混乱。

新版本的 OtterIO 通过 `cmd/config/identity/ldap/dn.go` 中的 `NormalizeDN`
把所有 DN 在进入 IAM 之前规范化:RDN 类型小写、值小写、空格折叠、
multi-valued RDN 内部按属性名排序。所有 IAM 边界(handler、`PolicyDBGet` /
`PolicyDBSet`、STS handler)都强制走规范化。

## 2. 这是一次性的破坏性变更

如果你的部署**确实**依赖了 DN 大小写差异作为不同身份处理(极少见,通常是
误用),升级后这些身份会被合并。请在升级前确认 IAM 映射表里没有"同一身
份两份不同策略"的情况。

## 3. 升级流程

### 3.1 dry-run(可选)

把环境变量 `OTTERIO_IAM_LDAP_DN_MIGRATION` 设为 `off`,然后启动:

```sh
export OTTERIO_IAM_LDAP_DN_MIGRATION=off
otterio server /data
```

这会让 OtterIO **只在内存里**做规范化和映射合并,**不动磁盘**。日志中会
出现 `LDAP DN migration` 字样的 info/error,可以从中判断是否会发生冲突。

### 3.2 正式迁移(默认行为)

把环境变量去掉(或显式设为 `on`)即可:

```sh
unset OTTERIO_IAM_LDAP_DN_MIGRATION
# 或者:
export OTTERIO_IAM_LDAP_DN_MIGRATION=on
otterio server /data
```

启动时,OtterIO 会扫描 `config/iam/policydb/users/*.json` 和
`config/iam/policydb/groups/*.json`(对象存储后端)或对应的 etcd 前缀
(etcd 后端),对其中文件名为非规范 LDAP DN 的条目执行:

1. **正常情况**(canonical 路径不存在):把内容写入 canonical 路径,删除
   原文件。
2. **重复内容**(canonical 路径已存在,内容相同):删除非规范副本。
3. **真正冲突**(canonical 路径已存在,内容**不同**):
   - 选 **lex-min** 的名字作为 winner(确定性,避免节点间不一致);
   - 把 loser 的内容**保留**为 `<原 path>.conflict-<unix-nano>` 兜底;
   - 在日志里以 ERROR 级别打印冲突告警,包含两侧路径与决定。

整个过程是**幂等**的:再次启动只会扫描到 canonical 文件,不再有迁移动作。

## 4. 冲突文件后处理

如果迁移后磁盘上出现 `*.conflict-*` 文件,运维需要人工评估:

```sh
# 列出所有冲突归档
mc ls --recursive otterio/.minio.sys/config/iam/policydb/ | grep conflict-
```

每个冲突文件对应一份"被合并掉的策略副本"。打开它(`madmin.DecryptData`
解密后是 `MappedPolicy` JSON)与 winner 比较,决定:

- 保留 winner、丢弃 conflict 副本 → 直接删 `*.conflict-*`;
- 应该保留的是 conflict 副本里的策略 → 用 `mc admin policy attach` 重设
  canonical key 的策略,再删 `*.conflict-*`;
- 二者都需要(应当属于不同账号) → 进 LDAP 检查为何两个账号有相同
  canonical DN,通常说明上游 LDAP 数据有问题。

## 5. 回滚选项

新版本不再保留"DN 字面量比较"的旧行为。如果你必须回滚:

1. 把二进制换回旧版;
2. 旧版不识别 `*.conflict-*` 文件,会忽略它们,但**不会**自动还原合并发生
   前的多份映射;原 `<非规范 DN>.json` 已经被迁移代码删除。

因此**强烈建议**先在 dry-run 模式确认无冲突,或在升级前手动备份
`config/iam/policydb/{users,groups}/` 下的所有文件。

## 6. 兼容矩阵

| 场景 | 升级前行为 | 升级后行为 |
|---|---|---|
| LDAP 返回的 DN 始终是同一种大小写 | 正常 | 正常 |
| LDAP 返回的 DN 大小写不一致 | 同一身份得到不同策略集 | 永远命中同一规范化 key |
| 管理员手工写过两份大小写不同的 mapping,内容相同 | 重复存储,但效果等价 | 自动合并为一份 |
| 管理员手工写过两份大小写不同的 mapping,内容**不同** | 实际效果取决于 LDAP 返回的大小写 | 选 lex-min 内容,另一份移到 `.conflict-*`,需人工处理 |
| LDAP 返回的 DN 包含非法语法(理论上不应发生) | 直接当字面量存表 | `Bind` / `LookupUserDN` 直接报错,登录失败 |

## 7. 相关代码

- `cmd/config/identity/ldap/dn.go` — `NormalizeDN`、`EqualDN`、
  `NormalizeDNSlice`。
- `cmd/config/identity/ldap/config.go` — `Bind` / `LookupUserDN` /
  `lookupUserDN` / `usernameFormatsBind` / `getGroups` / `Lookup` 出入口
  规范化。
- `cmd/iam.go` — `policyDBSet` / `PolicyDBGet` 边界二次规范化。
- `cmd/iam-object-store.go` — `migrateMappedPolicyToCanonical` /
  `archiveConflict` / `loadMappedPolicies` 持久化迁移。
- `cmd/iam-etcd-store.go` — etcd 后端的内存重键。
- `cmd/sts-handlers.go` — `AssumeRoleWithLDAPIdentity` invariant 检查。
- `cmd/config/identity/ldap/dn_test.go`、`cmd/iam_ldap_norm_test.go` —
  测试矩阵。

## 8. FAQ

**Q: 我从未配置 LDAP,这条迁移会影响我吗?**
A: 不会。迁移代码在 `globalIAMSys.usersSysType == LDAPUsersSysType` 的
情况下才执行;对内置用户系统(`OtterIOUsersSysType`)是完整 no-op。

**Q: STS / 服务账号 / 子账号的策略会被规范化吗?**
A: 不会。规范化仅适用于"`name` 是 LDAP DN"的代码路径,即 `regularUser`
和 `groups`。STS user / service account 的 access key 是字面量主键,继续走
原逻辑。

**Q: `OTTERIO_IAM_LDAP_DN_MIGRATION=off` 在生产可以长期保持吗?**
A: 不推荐。关闭迁移意味着内存里规范化、磁盘上分散——下次重启又要重做合
并。这只是用来 dry-run / 审计用的开关。
