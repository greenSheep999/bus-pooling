# Supported Regions

**Effective Date: August 10, 2026**

This page describes where you can use Kiro.bus, and what the constraints are on payment, upstream Vendor availability, and cross-border use. The Service is delivered globally over HTTPS, but three separate layers control who can actually use it end-to-end: **the platform**, **the payment gateway**, and **the upstream Vendor and model provider**.

## I. Overview

Kiro.bus itself is a routing and pooling tool. It runs on the public internet and is available from any region that has open internet access. What limits real-world availability is not our platform, it is:

1. **The payment gateway.** Kiro.bus accepts top-ups in **USD** through a third-party payment gateway (currently `waffo`). The gateway's own coverage determines where a top-up can be completed.
2. **The upstream Vendor.** Each Vendor decides where its keys and its underlying account may be used. If a Vendor has geographic restrictions, they pass through to you.
3. **The upstream model provider.** Anthropic, OpenAI, Google, AWS, and their peers each have their own list of supported and prohibited regions. Those restrictions travel through the Vendor to the key you end up holding.

You must comply with the strictest of those three layers for any given route.

## II. Where the platform works

- The Kiro.bus website, dashboard, API, and webhook endpoints are available globally over HTTPS.
- We do not currently apply platform-level geographic blocks.
- Users may register an account and hold credits from any region that can complete a top-up through the supported payment gateway.

## III. Where top-ups can be completed

Top-ups are processed by `waffo` (or a successor gateway we announce in the docs) and are billed in **USD**. The gateway's own coverage list governs whether a top-up can be completed from your location. In practice this typically includes:

- **North America.** United States, Canada.
- **Europe.** United Kingdom, Ireland, and most EU/EEA member states.
- **Asia-Pacific.** Japan, South Korea, Singapore, Hong Kong SAR, Taiwan, Australia, New Zealand, Malaysia, Thailand, Indonesia, the Philippines.
- **Latin America.** Mexico, Brazil, Argentina, Chile.
- **Middle East.** United Arab Emirates, Saudi Arabia, Israel.

If your card or wallet is issued outside a covered region, the top-up may be declined by the gateway, the issuing bank, or both. This is a decision of the gateway and the issuer — Kiro.bus does not add a layer of geographic screening on top of theirs.

## IV. Where Vendor keys can be used

Each Vendor decides where the keys it issues may be used and where its underlying accounts are permitted. Common Vendor restrictions include:

- Regions covered by U.S., U.K., EU, or other export-control sanctions;
- Regions where the underlying model provider does not offer service;
- Regions where the Vendor's own payment or KYC operator does not operate.

Because Kiro.bus does not manufacture keys, we cannot override Vendor geographic policy. If a Vendor rejects a request based on region, that decision is not appealable through Kiro.bus.

## V. Model provider restrictions

The upstream model providers (Anthropic, OpenAI, Google, AWS, and others) publish their own country and territory lists. If the model provider does not offer service in your region, a Vendor's key that routes to that provider will not work in your region — even if the platform and gateway are otherwise available.

We do not maintain a mirror of every provider's list. Please check the current supported-country list of the model provider you intend to use before running production traffic.

## VI. Cross-border considerations

- **Using a key from one region while located in another.** This is governed by the Vendor's and the model provider's policies, not by us. Some providers explicitly forbid this. It is your responsibility to know what your route allows.
- **Sanctions and export control.** You may not use Kiro.bus, or credits or keys obtained through Kiro.bus, in violation of applicable sanctions or export-control laws. This applies regardless of whether the payment gateway or a Vendor happens to allow a specific transaction.
- **Local rules for AI use.** Some jurisdictions have AI-specific regulations (EU AI Act, various U.S. state laws, sector-specific rules). Compliance with those rules is your responsibility as the end user of the key.

## VII. If your region is not covered

If you cannot register, top up, or use Kiro.bus from your region, the constraint is almost always in one of the three layers above rather than in our own configuration. The fastest path is to check:

1. Whether the payment gateway supports your country and your specific card / wallet issuer.
2. Whether the Vendor you want to route through supports your region.
3. Whether the underlying model provider supports your region.

You can also open an issue on the [GitHub repository](https://github.com/greenSheep999/kiro.bus) or ask in the [Community](/community) channels. In some cases the community will know a workaround (e.g., a different Vendor that supports the region); in others the answer is "not today". We do not sell region access separately from what the payment gateway and Vendors already provide.

## VIII. Policy updates

This page changes as payment gateways, Vendors, and model providers change their own coverage. We keep it up to date on a best-effort basis; the authoritative list for each layer is always the provider's own documentation. New versions of this page take effect on publication.
