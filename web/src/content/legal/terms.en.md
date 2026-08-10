# Terms of Service

**Effective Date: August 10, 2026**

Welcome to Kiro.bus. Please read these terms before you use the Service.

These Terms of Service (the "Terms") form an agreement between you (the "User") and the community operating Kiro.bus (collectively, "we", "us", "our") regarding your use of the Kiro.bus website, dashboard, API, webhook endpoints, and related tools (collectively, the "Service"). By registering an account, generating an API key, or otherwise using the Service in any way, you are deemed to have read, understood, and agreed to these Terms.

## What Kiro.bus is (and is not)

Kiro.bus is an **open-source, community-built pooling tool** for API keys. Its job is limited and specific:

- **We do not produce API keys.** All keys served through Kiro.bus are pulled from independent upstream vendors ("Vendors"). We are not the issuer, publisher, or reseller of those keys, and we do not have any ownership stake in the Vendors.
- **We route, we do not manufacture.** Every service the platform performs — fetching a key, routing, health probing, warranty refills, ledger — is a routing and accounting layer on top of Vendors and the open-source key-management project `kiro.rs`. Both the Vendors and `kiro.rs` are independent third parties.
- **We are not a Vendor and do not endorse Vendors.** Availability, price, quality, lifespan, and terms of any key are decisions made by the issuing Vendor. We publish live availability and effective-cost signals so riders can choose, but we make no representation about a Vendor's business, longevity, or output.
- **We are not affiliated with the upstream model providers.** Mention of Anthropic, OpenAI, Google, AWS, or any specific model, product, or trademark in the Service is descriptive only. Kiro.bus is not authorized, endorsed, or sponsored by any of them.

If you require an SLA, a contract with a Vendor, or first-party support from a model provider, Kiro.bus is not the right tool — go directly to that Vendor or provider.

## A. Who may use the Service

The Service is intended for **individual users** who want to pool the cost of Vendor keys with other individuals or use the Service to fetch a key for themselves.

- We do not currently support resellers, white-label operators, or multi-tenant redistribution. Repackaging or reselling the Service to a downstream customer base is not permitted without our prior written authorization.
- You must be of legal age to form a contract in your jurisdiction. If you are using the Service on behalf of an organization, you represent that you have authority to bind that organization.

## B. Account and credentials

- Registration information must be true, accurate, and complete, and you must keep it up to date.
- You are responsible for safeguarding your password and API keys. Every request made from your account or with your key is deemed to be your action.
- If you discover an account anomaly, a leaked key, or unauthorized use, revoke the key from the dashboard immediately and notify us.
- We may require lightweight verification (email confirmation, human-check, occasional rate reviews) to prevent fraud and abuse.

## C. How the Service works

- **Fetching a key.** You request a key; we fetch one from a Vendor and either hand it to you as plaintext, put it into a pool you own, or place it inside a "bus" you share with other riders. Every fetch is a routing action against the Vendor.
- **Bus (pool).** Riders on the same bus share the cost of the same underlying Vendor account. Concurrency, rate limits (RPM/TPM), and quotas are metered per Vendor account and are therefore shared by everyone on the bus. This is a property of the Vendor's account, not something we control.
- **Warranty and refill.** For each key issued, we monitor liveness for a warranty window. If the key fails inside the warranty window, we automatically fetch a replacement and refund the used credits pro rata. Warranty is limited to what our probes can detect; we do not warrant the Vendor's uptime, price, or general fitness for purpose.
- **Handoff (plaintext extraction).** If you choose to extract a key as plaintext, we deliver it to you once and immediately delete our copy. After handoff we no longer monitor, refill, or refund that key — it is entirely in your hands.
- **Ledger.** Every credit event (top-up, pull, refill, refund) is written to your wallet ledger, which is exportable from the dashboard at any time.

## D. Vendors, upstream policies, and pass-through

- **Vendor terms pass through to you.** When the Service routes a request to a Vendor, you are simultaneously accepting that Vendor's terms of service, acceptable-use policy, and any regional or commercial restrictions on the underlying model or account. It is your responsibility to know and comply with those terms.
- **We follow Vendor policy on a best-effort basis.** We use commercially reasonable efforts to reflect Vendor availability, price changes, and lifecycle events, but we are not liable for changes made by any Vendor. If a Vendor suspends or terminates our access, we may adjust or drop the affected routes.
- **Model provider policies (Anthropic, OpenAI, Google, AWS, etc.) pass through to you** through the Vendor. If a model provider requires downstream disclosures, geographic restrictions, or content policies, those obligations are yours as the end user of the key.
- **We do not train on your inputs or outputs.** We do not read, retain, or use the content of your API requests to train any model, for any party.

## E. Credits, top-ups, and refunds

- The Service is billed in **credits** (`1 credit ≈ $0.14`, billed in USD). Every line of a top-up (credits, payment-gateway fee, arrival amount) is shown on your wallet page before you pay.
- The payment gateway fee is a **pass-through** we collect on the gateway's behalf. Credits themselves are ours.
- Refund rules:
  - **Change of mind:** if you have not used the credits, you may request a refund within 30 days of the top-up. Refunds go back through the original payment channel less any non-recoverable gateway fees.
  - **Warranty refunds:** automatic and credited to your wallet. They are not paid out to your card.
  - **After 90 days:** refunds are subject to identity verification and may be declined for anti-fraud reasons.
  - **Credits already spent** on completed key fetches, handoffs, or shared-bus rounds are non-refundable.

## F. Prohibited conduct

You must comply with the [Usage Policy](/legal/usage) and the [Service-Specific Terms](/legal/services). Without limiting those policies, you must not:

- Use the Service in any way that violates the laws or regulations of your country or region;
- Resell, sublicense, or redistribute the Service, or offer it on a multi-tenant basis, without our prior written authorization;
- Use the Service to train, distill, fine-tune, or evaluate any AI model that competes with the upstream models or with the Vendors themselves;
- Circumvent our billing, rate limits, quotas, warranty logic, or fraud controls;
- Register multiple accounts to defeat rate limits, farm referral credits, or evade a suspension;
- Reverse-engineer, scan, scrape, bulk-extract, or perform unauthorized access against the Service;
- Interfere with the Service's ability to monitor Vendor liveness, or attempt to manipulate the warranty logic to obtain refunds you would not otherwise be entitled to;
- Use the Service to enable any activity that a Vendor or upstream model provider has explicitly forbidden.

## G. Suspension and termination

- We may suspend or terminate your access, with or without notice, if (a) you breach these Terms or any linked policy, (b) we reasonably believe a security, fraud, or compliance risk exists, (c) an upstream Vendor terminates or restricts access, or (d) an account has been inactive or in arrears for an extended period.
- You may close your account from the dashboard at any time. Unused credits at closure follow the refund rules above.
- Warranty windows that begin before termination survive termination until they expire naturally.

## H. Disclaimers

- **The Service is provided "as is" and "as available".** We make no express or implied warranty of continuous availability, error-free operation, fitness for a particular purpose, or accuracy of any Vendor's outputs.
- **We do not guarantee Vendor quality.** Key availability, useful lifespan, quota, model version, output quality, and legal compliance are the Vendor's responsibility. We publish observed signals for your decision-making; we are not the source of truth.
- **We do not guarantee upstream availability.** If a Vendor goes down, changes prices, or removes a model, we will route around it where we can, but we make no promise of continuous service on any particular Vendor.
- **Best-effort tool.** Kiro.bus is a community-run best-effort tool. It is not a certified financial, legal, or commercial-grade service.

## I. Limitation of liability

To the maximum extent permitted by applicable law:

- Our total aggregate liability to you for any claim arising out of or related to the Service is capped at the **credits you spent on the Service in the ninety (90) days preceding the claim**.
- We are not liable for indirect, incidental, special, consequential, exemplary, or punitive damages, including loss of profits, data, or goodwill, even if we were advised of the possibility of such damages.
- We are not liable for any act, omission, price change, service change, content policy, or business decision by any Vendor or upstream model provider.

## J. Open-source components

Kiro.bus is open source. The pooling protocol and CLI-style extraction path are released under a permissive license (see the repository). The downstream key-management service `kiro.rs` is a separate open-source project we do not maintain; we operate an instance of it as a substrate, but we do not own, maintain, or govern `kiro.rs`. You may run your own instance, contribute, or fork it under its own license — please search for the project directly.

## K. Privacy

Our handling of your account information, wallet ledger, and operational logs is described in the Privacy Policy, which is incorporated into these Terms by reference. Content that passes through Kiro.bus in the course of routing a request (API request bodies, outputs) is **not** read, retained, or trained on by us.

## L. Changes

We may update these Terms from time to time. Material changes will be announced via the top banner or a dashboard notice at least thirty (30) days before they take effect. Your continued use of the Service after that period constitutes acceptance.

## M. Governing law and disputes

If a dispute arises, please contact us first through the community channels listed on the [Community](/community) page — the fastest path to resolution is almost always a direct conversation. If a formal remedy is required, the dispute will be resolved in the courts of the jurisdiction of our operating maintainer, and each party will bear its own costs.

## N. Miscellaneous

- If any provision of these Terms is held invalid or unenforceable, the remaining provisions remain in full force.
- These Terms constitute the entire agreement between you and us regarding the Service and supersede any prior agreement on the same subject.

Questions? Post in the [Community](/community) channels or open an issue on the [GitHub repository](https://github.com/greenSheep999/kiro.bus).
