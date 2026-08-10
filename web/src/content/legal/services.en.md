# Service-Specific Terms

**Effective Date: August 10, 2026**

These Service-Specific Terms supplement the [Terms of Service](/legal/terms) and describe how specific features and integrations work. If a Service-Specific Term conflicts with the Terms of Service, this document controls **for the specific matter it covers**; everything else continues to be governed by the Terms.

## A. Pass-through of Vendor terms

Kiro.bus routes each key fetch to an independent upstream Vendor. When your request routes to a Vendor:

- **You accept the Vendor's terms** — the Vendor's terms of service, acceptable use policy, content policy, commercial license, and any regional restrictions apply to your use of the key.
- **Vendor changes flow through automatically** — availability, price, quota, model version, output policies, and lifecycle events change on the Vendor's schedule. We do not have advance notice and cannot preempt them.
- **We are not liable for Vendor actions.** If a Vendor blocks your request, retires a model, changes prices, or terminates our access, we will route around the failure where possible; the underlying decision is not ours to make or reverse.

## B. Pass-through of upstream model provider terms

Vendors, in turn, route to model providers (Anthropic, OpenAI, Google, AWS Bedrock, Azure, Vertex, and others as applicable). Those providers' policies pass through to you via the Vendor:

- Anthropic's Usage Policy, OpenAI's Usage Policies, Google's model policies, AWS Acceptable Use Policy, and any equivalent policy apply to the extent your key routes to that provider.
- If a model provider explicitly forbids a use case, you must not use their route for that use case, even if the Vendor's own terms are silent.
- We do not warrant that a given Vendor's account with a given model provider will remain in good standing; that is between the Vendor and the model provider.

## C. Protocols and compatibility

Kiro.bus's gateway is protocol-agnostic. When you fetch a key from Kiro.bus, the key is a standard API key issued by the Vendor. From that point on, protocol compatibility is a property of the Vendor and the model provider, not us:

- The key works with whatever tools and protocols the upstream supports (OpenAI-compatible, Anthropic Messages, Claude Code, Codex, Bedrock, Vertex, etc.). We make no additional guarantee about protocol coverage or SDK behavior.
- We make commercially reasonable efforts to keep our own routing and webhook payloads stable and backwards-compatible; when we do change them, we announce it in the changelog and, where possible, run a compatibility window.

## D. Bus (pooling) mechanics

- **One Vendor account per bus.** All riders on the same bus share the same underlying Vendor account. This is what makes pooling cheap; it also means concurrency, RPM, TPM, and quota are shared. This is a property of the Vendor's account, not a bug in our scheduler.
- **Cost splitting.** By default, cost is split by headcount for each round of the bus. Rules for uneven splits (e.g., by usage share, by a rider pushing their own key into the bus) are described on the bus detail page.
- **Push-your-own-key credit.** If you push a key you already own into a bus, other riders' consumption of that key credits your wallet 1:1 in credits. Balances are settled at the end of each round.
- **Suspension for insufficient balance.** If your wallet cannot cover a round, that round skips you (the bus keeps running, but you get no key). Three consecutive skipped rounds pauses your seat and stops billing your share until you top up.
- **Leaving mid-round.** The current round is not refunded — the key has already been pulled. From the next round on, you are no longer billed and no longer receive new keys. Keys you already received are yours.

## E. Warranty and refill

- **Warranty window.** Each key comes with a warranty window during which we monitor liveness. If the key fails inside that window, we automatically fetch a replacement and refund the used credits pro rata.
- **Refill triggers a webhook.** Every refill, refund, and status change fires a webhook (see [/docs](/docs) for payloads). Downstream automation should treat these events as authoritative.
- **What warranty does not cover.** Warranty is limited to failure signals our probes can observe (auth failure, hard-block, out-of-quota response). It does not cover Vendor policy changes, model retirements, degraded output quality, subjective performance concerns, or the account-level state of the Vendor.

## F. Handoff (plaintext extraction)

- **One-shot delivery.** When you choose plaintext extraction, we deliver the key once and immediately delete our copy.
- **No warranty after handoff.** Once the key is in your hands, we cannot monitor it, refill it, or refund it. If you need warranty and refill, keep the key in your own pool or on a bus.
- **Ledger persists.** We keep a masked handoff record (vendor, timestamp, consumed credits) in your ledger for support and audit purposes; the plaintext itself is never retained. See Terms §J for retention details.

## G. Push to your own pool

- You can point Kiro.bus at a passenger-owned pool (URL + token in Settings) and every key we pull for you is written to your pool as well as monitored on Kiro.bus.
- **Dual write.** The key is in both places at once. When it dies, we still refill and refund.
- Your pool is your infrastructure. We do not accept responsibility for its availability, security posture, or data.

## H. API keys, webhooks, and automation

- **API keys.** You may create API keys with the `usr-` prefix in your dashboard. Each key inherits your account's rate limits, quotas, and policies. Requests made with an API key are attributed to your account.
- **Webhook signatures.** All outbound webhooks are signed with your webhook secret. Verify the signature server-side; unsigned or invalid deliveries should be discarded.
- **Delivery retries.** Failed deliveries retry with exponential backoff for a bounded number of attempts. See [/docs](/docs) for the current retry schedule.
- **Rate limits.** We publish per-endpoint rate limits in the docs. Sustained overshoot triggers throttling and, eventually, temporary suspension of the offending API key.

## I. Beta and experimental features

- Features marked "beta", "preview", or "experimental" are provided **as-is** with no stability guarantee.
- We may change or remove beta features without notice. They are not appropriate for production dependencies.
- Our liability for beta features is capped at the lesser of USD 50 and the credits you spent on the beta feature in the preceding thirty (30) days.

## J. Availability targets

- We target **99% monthly uptime** for the Kiro.bus gateway itself (the API layer, the dashboard, and webhook delivery). This is a target, not a contractual SLA.
- **Vendor availability is out of scope.** If a Vendor is down, Kiro.bus's job is to route around it — not to keep the failing Vendor working.
- Scheduled maintenance is announced through the top banner and the community channels at least 24 hours in advance where feasible.

## K. Data retention

- **Account, wallet, and ledger records** are retained for as long as your account is open, and for a reasonable period after closure for anti-fraud and audit purposes.
- **Warranty logs** (which requests were probed alive vs. failed) are retained for the length of the warranty window and a short buffer, and then discarded.
- **Handoff records** (masked handoff metadata) are retained per Terms §J for support and audit; the plaintext key material itself is deleted on delivery.
- **Request bodies and model outputs** that pass through the routing layer are not retained beyond the transient window needed to deliver the request.

## L. Fine-tuning, training, and derivative use

- We do not use your inputs or outputs to train any model, for ourselves or any third party.
- Vendor and model provider policies about fine-tuning, distillation, and derivative model creation pass through to you — you must comply with them.
- Using Kiro.bus to train, distill, fine-tune, or benchmark a model that competes with the Vendors or the model providers is prohibited (see Terms §F).

## M. Termination and material change

We may terminate or materially change a specific feature if:

- The upstream Vendor is retired or ceases to supply us;
- The feature is found to violate applicable laws or upstream policies;
- The feature is abused or is no longer sustainable to operate.

We provide reasonable advance notice through the top banner, the community channels, or dashboard notifications, and offer an equivalent alternative where one exists.

## N. Updates

We may update these Service-Specific Terms from time to time. Material changes take effect thirty (30) days after announcement. Continued use of the affected feature after that constitutes acceptance.

If any provision here is held invalid or unenforceable, the remaining provisions remain in full force.
