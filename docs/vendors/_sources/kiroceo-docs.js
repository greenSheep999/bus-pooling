import { html } from '../../vendor/preact.js';
import { app } from '../session.js';
import { useStore } from '../store.js';
import { Btn, Card, Notice, PageHead, copyWithToast } from '../ui.js';
import { Icon } from '../icons.js';

/**
 * 对接文档。
 *
 * 写成页面而不是给一份外部文档，是因为里面的 base URL、密钥占位、区域单价
 * 都跟当前这套部署有关 —— 抄一份静态文档出去，很快就会和实际接口对不上。
 * 这里的示例直接用当前站点的地址生成，复制就能跑。
 */

const SEC = [
  { id: 'auth', label: '鉴权' },
  { id: 'quota', label: '额度与积分' },
  { id: 'api', label: '接口清单' },
  { id: 'flow', label: '自动提货流程' },
  { id: 'webhook', label: 'Webhook' },
  { id: 'errors', label: '错误码' },
];

/** 代码块 + 常驻复制按钮。文档里的示例就是拿来抄的。 */
const Code = ({ children }) => html`
  <div class="code-block">
    <pre>${children}</pre>
    <span class="cb-copy">
      <button
        class="btn btn-ghost btn-icon btn-sm"
        data-tip="复制"
        aria-label="复制"
        onClick=${() => copyWithToast(children, '已复制到剪贴板')}
      >
        <${Icon} name="copy" />
      </button>
    </span>
  </div>
`;

const Api = ({ method, path, desc }) => html`
  <div class="api-row">
    <div class="ar-head">
      <span class=${`ar-method ${method.toLowerCase()}`}>${method}</span>
      <span class="ar-path">${path}</span>
    </div>
    <div class="ar-desc">${desc}</div>
  </div>
`;

const Fields = ({ rows }) => html`
  <table class="field-table">
    <thead>
      <tr><th>字段</th><th>类型</th><th>说明</th></tr>
    </thead>
    <tbody>
      ${rows.map((r) => html`<tr key=${r[0]}><td>${r[0]}</td><td>${r[1]}</td><td>${r[2]}</td></tr>`)}
    </tbody>
  </table>
`;

export function DocsPage() {
  const { cfg, me } = useStore(app);
  const base = location.origin;
  // 示例里用占位符而不是真密钥：这一页经常被截图或转发出去。
  const key = 'YOUR_API_KEY';

  const zones = (cfg.zones || []).map((z) => `${z.label}(${z.zone}) ${z.unit_price} 积分/个`).join('，');

  return html`
    <${PageHead} title="对接文档" sub="用程序自动提货与接收到货通知" />

    <div class="doc-toc">
      ${SEC.map((s) => html`<a key=${s.id} href=${`#/docs`} onClick=${(e) => { e.preventDefault(); document.getElementById(s.id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>${s.label}</a>`)}
    </div>

    <!-- 鉴权 -->
    <div id="auth" class="doc-sec">
      <${Card} title="鉴权">
        <p>
          所有接口用请求头 <code class="inline">X-API-Key</code> 携带你的访问密钥，没有额外的登录步骤、也不需要换 token。
          密钥可以在「账户」页查看，忘了随时能再看。
        </p>
        <${Code}>${`curl -H "X-API-Key: ${key}" ${base}/api/my/profile`}<//>
        <${Notice} tone="warn" icon="alert">
          密钥等同于你的账号凭据，不要写进前端代码或提交到代码仓库。泄露后请联系管理员轮换。
        <//>
      <//>
    </div>

    <!-- 额度 -->
    <div id="quota" class="doc-sec">
      <${Card} title="额度与积分">
        <p>
          本站按<b>积分</b>计费，不再按「还能提几个号」。每个区域单价独立设置，当前是 ${zones || '（尚未配置）'}。
          提货时扣的积分 = 该区单价 × 实际成交数量。
        </p>
        <${Notice} tone="info" icon="info">
          如果你之前对接的是旧版接口：字段名一个都没改（<code class="inline">quota</code>、
          <code class="inline">remaining</code>、<code class="inline">used_quota</code>、
          <code class="inline">purchased</code>），只是这些数字现在表示积分而不是个数。
          原来的代码不用改就能继续跑。
        <//>
        <${Notice} tone="ok" icon="shield">
          <b>10 分钟质保</b>：提货后 10 分钟内，如果号被系统检测到失效（封号），
          会<b>自动</b>把这单实际扣的积分退还到你的余额，无需申请。退款按下单时的单价计算，
          在积分明细里记为 <code class="inline">refund</code>。超过 10 分钟才失效的属于正常损耗，不退。
        <//>
      <//>
    </div>

    <!-- 接口清单 -->
    <div id="api" class="doc-sec">
      <${Card} title="接口清单">
        <p>下面这组 <code class="inline">/api/my/*</code> 与旧版完全兼容。<code class="inline">/api/me/purchase</code> 与 <code class="inline">/api/my/purchase</code> 接受同样的请求体、返回同样的兼容结构，可以任选其一。</p>

        <${Api} method="GET" path="/api/my/profile" desc="账号信息与积分余额" />
        <${Api} method="GET" path="/api/my/stock" desc="可提取数量、单次上下限、各区单价与可购量（zones 数组）" />
        <${Api} method="POST" path="/api/my/purchase" desc="提货。带 client_order_id 作为幂等键，zone 可选（不传默认美国区，可传 us / eu）。等价于 /api/me/purchase" />
        <${Api} method="GET" path="/api/my/keys" desc="我的凭据。带 ?history=1 时含已失效的" />
        <${Api} method="GET" path="/api/my/purchase-orders" desc="历史订单" />
        <${Api} method="GET" path="/api/my/keys/usage" desc="用量采样（每分钟积分消耗）" />
        <${Api} method="POST" path="/api/my/redeem" desc="兑换码换积分" />
        <${Api} method="PUT" path="/api/my/webhook" desc="保存到货通知地址" />

        <h3 style="font-size:13.5px;font-weight:650;margin:18px 0 8px">提货请求</h3>
        <${Code}>${`curl -X POST ${base}/api/my/purchase \\
  -H "X-API-Key: ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{"count": 5, "zone": "us", "client_order_id": "0123456789abcdef0123456789abcdef"}'`}<//>

        <${Notice} tone="info" icon="info">
          <b>zone 参数按区严格隔离，不跨区补货：</b>
          <ul style="margin:6px 0 0;padding-left:18px">
            <li><b>不传 zone</b> → 默认只从<b>美国区</b>取号；美国区缺货就返回缺货，<b>不会</b>用欧洲区的号顶上。</li>
            <li><b>zone: "us"</b> → 只拿美国区。</li>
            <li><b>zone: "eu"</b> → 只拿欧洲区。</li>
            <li>传入 us / eu 以外的值 → 直接 400 报错，不会静默按美国区处理。</li>
          </ul>
          换句话说：想要欧洲区的号，必须显式传 <code class="inline">zone: "eu"</code>；只有这一种方式能拉到欧洲区。
        <//>

        <h3 style="font-size:13.5px;font-weight:650;margin:18px 0 8px">提货响应</h3>
        <${Code}>${`{
  "client_order_id": "0123456789abcdef0123456789abcdef",
  "purchased": 5,              // 实际成交数量，可能小于 count
  "remaining": 4500,           // 扣后积分余额
  "keys": [                    // 对象数组，每个元素是一个可用凭据
    {
      "key": "kiro-xxx",
      "account": "user@example.com",
      "password": "...",
      "issuer_url": "https://..."
    }
  ],
  "zone": "us",                // 以下为新增字段，老客户端可忽略
  "unit_price": 100,
  "total_credits": 500,
  "order_id": "a1b2c3..."
}`}<//>

        <${Notice} tone="warn" icon="alert">
          <b>务必按 <code class="inline">purchased</code> 而不是 <code class="inline">count</code> 处理结果。</b>
          库存是并发争抢的，申请 5 个拿到 3 个是正常结果，只按实际成交数量扣费。
          <br />
          <code class="inline">keys</code> 是对象数组，每个元素含
          <code class="inline">key / account / password / issuer_url</code> 四个字段，
          直接遍历取用即可。
        <//>

        <h3 style="font-size:13.5px;font-weight:650;margin:18px 0 8px">幂等键规则</h3>
        <p>
          <code class="inline">client_order_id</code> 必须是 32 位十六进制（也可以用
          <code class="inline">Idempotency-Key</code> 请求头传）。<b>网络超时后请用同一个 id 重试</b>——
          服务端会识别成同一笔订单原样返回，不会重复扣费、重复发货。换一个新 id 重试则会变成第二笔订单。
        </p>
      <//>
    </div>

    <!-- 自动提货流程 -->
    <div id="flow" class="doc-sec">
      <${Card} title="自动提货流程">
        <p>推荐的做法是「收到通知再提货」，而不是定时轮询：</p>
        <${Code}>${`1. 配好 webhook 地址（本页下方可以直接模拟推送来联调）
2. 收到 new_keys_available 事件
3. 取出事件里的 purchase_order_id
4. 用它作为 client_order_id 调 /api/my/purchase
   —— 这样即使你的服务重启、事件被重复投递，也只会成交一次
5. 按响应里的 purchased 和 keys 入库

如果一定要轮询，先查 /api/my/stock 的 max 字段，大于 0 再提货，
间隔不要短于 30 秒。`}<//>
        <${Notice} tone="ok" icon="ok">
          直接把事件里的 <code class="inline">purchase_order_id</code> 当
          <code class="inline">client_order_id</code> 用，是最省事的幂等做法：
          webhook 可能重投，而同一个 id 只会成交一次。
        <//>
      <//>
    </div>

    <!-- Webhook -->
    <div id="webhook" class="doc-sec">
      <${Card} title="Webhook">
        <p>
          配置地址后，系统会在两种情况下向你 <code class="inline">POST</code> 一个 JSON。
          失败会自动重试 3 次（间隔 3s / 8s），请在 10 秒内返回 2xx。
        </p>

        <h3 style="font-size:13.5px;font-weight:650;margin:16px 0 8px">事件类型</h3>
        <${Fields}
          rows=${[
            ['new_keys_available', 'string', '有新号入库。带 purchase_order_id，直接拿去当提货的幂等键。'],
            ['all_keys_dead', 'string', '你名下的号本轮全部失效，系统正在自动补货。'],
            ['test', 'string', '你点「发送测试」时推的，仅用于验证地址可达。'],
          ]}
        />

        <h3 style="font-size:13.5px;font-weight:650;margin:16px 0 8px">载荷字段</h3>
        <${Fields}
          rows=${[
            ['event', 'string', '事件类型，见上表'],
            ['event_id', 'string', '事件唯一 id，可用于去重'],
            ['purchase_order_id', 'string', '仅 new_keys_available 有。作为 client_order_id 提货'],
            ['pool_id', 'string', '触发本次通知的母号 id。同一母号的重复通知按它去重，避免重复拉取；全死事件涉及多个母号时用逗号连接'],
            ['message', 'string', '给人看的中文描述'],
            ['new_keys', 'int', '仅 new_keys_available 有。新增数量'],
            ['dead', 'int', '仅 all_keys_dead 有。失效数量'],
            ['zone', 'string', '区域（us / eu），仅补货事件有'],
          ]}
        />

        <h3 style="font-size:13.5px;font-weight:650;margin:16px 0 8px">新号到货</h3>
        <${Code}>${`{
  "event": "new_keys_available",
  "event_id": "7f3a9c2e1b4d5a6f8e9c0b1a2d3e4f5a",
  "purchase_order_id": "7f3a9c2e1b4d5a6f8e9c0b1a2d3e4f5a",
  "pool_id": "a1b2c3d4e5f6",
  "message": "美国区新增 20 个 Key 已就绪；...",
  "new_keys": 20,
  "zone": "us"
}`}<//>

        <h3 style="font-size:13.5px;font-weight:650;margin:16px 0 8px">全部失效</h3>
        <${Code}>${`{
  "event": "all_keys_dead",
  "event_id": "3c8d1f0a5b7e2694c1d8a0f3b5e7c9d2",
  "message": "本轮全部 12 个 Key 已失效，系统正在自动补充新账号",
  "dead": 12
}`}<//>

        <${Notice} tone="info" icon="info">
          去「账户」页保存 webhook 地址时，可以直接<b>模拟推送这两种事件</b>，
          载荷与真实事件完全一致（只在 message 里标了「[模拟]」），方便你先把接收端调通。
        <//>
      <//>
    </div>

    <!-- 错误码 -->
    <div id="errors" class="doc-sec">
      <${Card} title="错误码">
        <p>出错时响应体是 <code class="inline">{"error": "中文说明"}</code>，状态码含义如下：</p>
        <${Fields}
          rows=${[
            ['400', '参数错误', '幂等键格式不对、区域非法、数量越界'],
            ['401', '密钥无效', '检查 X-API-Key，或密钥已被轮换'],
            ['402', '积分不足', '先兑换积分再提货'],
            ['403', '账号被停用', '联系管理员'],
            ['404', '不存在', '查询的资源不属于你，或已被删除'],
            ['409', '状态冲突', '库存不足、已达最大持有库存上限、幂等键撞了别的订单；用同一个 id 重试'],
            ['410', '兑换码过期', '换一张'],
            ['429', '过于频繁', '降低频率后重试'],
            ['5xx', '服务端错误', '用同一个 client_order_id 重试是安全的'],
          ]}
        />
        <${Notice} tone="warn" icon="alert">
          遇到 5xx 或网络超时时，<b>请用同一个 client_order_id 重试</b>而不是换新的——
          订单可能已经成交，换 id 会变成第二笔。
        <//>
      <//>
    </div>

    <div class="row" style="margin-top:4px">
      <${Btn}
        kind="subtle"
        icon="copy"
        onClick=${() => copyWithToast(`${base}/api/my`, '接口地址已复制')}
      >
        复制接口地址
      <//>
      <span class="faint">当前站点：${base}${me ? ` · 账号 ${me.name}` : ''}</span>
    </div>
  `;
}
