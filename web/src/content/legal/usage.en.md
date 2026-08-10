# Usage Policy

**Effective Date: August 10, 2026**

This Usage Policy (also called the Acceptable Use Policy or "AUP") applies to everyone who sends requests through Kiro.bus, including individual users pulling keys, riders sharing a bus, and anyone integrating the platform via API or webhook.

The Policy exists for two reasons: keep users safe, and stay compatible with the terms that upstream Vendors and model providers pass through to us. We enforce it with automated detection, manual review, and traffic monitoring. Violations may result in blocked content, rate limiting, temporary suspension, permanent termination, or referral to law enforcement.

## I. Universal standards

These apply to every user and every use case. When you use Kiro.bus, you may not:

### 1. Break the law
Use the Service to engage in or assist any activity that violates the laws of your country or region, including illegal drugs, weapons trafficking, human trafficking, child sexual exploitation, money laundering, IP infringement, harassment, fraud, gambling, or pyramid schemes.

### 2. Attack critical infrastructure
Attack, damage, or disrupt critical infrastructure — power, telecom, healthcare, transportation, water, finance, elections, government systems, or their control systems.

### 3. Compromise computer or network systems
Develop, assist, or automate: vulnerability exploitation, social engineering, malware, ransomware, denial-of-service attacks, botnets, unauthorized intrusion, security-control bypass, or large-scale automation designed to harm other people's systems.

### 4. Build weapons or aid military targeting
Research, design, produce, test, or procure weapons, explosives, or hazardous materials. This includes command-and-control, targeting, autonomous weapons, and CBRN (chemical, biological, radiological, nuclear) work.

### 5. Incite violence or hatred
Incite, promote, or celebrate violence against any individual or group, or dehumanize, harass, or discriminate against people based on protected characteristics (race, ethnicity, religion, gender, sexual orientation, disability, etc.).

### 6. Violate privacy or identity rights
Stalk, harass, or bully individuals. Collect, aggregate, or sell others' personal information, biometric data, health data, precise location, or communications without consent. Impersonate real people or generate content designed to be mistaken for a specific real person.

### 7. Harm minors
Create, distribute, or facilitate any content that sexualizes, exploits, grooms, or endangers minors. No exceptions. Offenders are banned and reported to the appropriate authorities.

### 8. Cause psychological harm
Auto-generate or distribute content that promotes self-harm, suicide, eating disorders, or targeted psychological abuse.

### 9. Manufacture or amplify disinformation
Mass-produce or spread deliberately false, misleading, or fabricated content, including deepfakes and content targeting public health, elections, judicial processes, public policy, or public officials.

### 10. Interfere with political processes
Interfere with elections, fabricate public opinion at scale, run automated lobbying or voter contact while concealing that it is automated, or generate political campaign material at scale.

### 11. Enable prohibited surveillance
Build predictive policing systems, sentencing/parole/detention decision aids, social credit scoring, facial identification for tracking private individuals, biometric emotion recognition, mass surveillance, or battlefield command systems.

### 12. Engage in fraud or deception
Phishing, identity forgery, fake reviews, spam, forged documents, fake transactions, pyramid schemes, or any deceptive, misleading, or exploitative behavior toward others.

### 13. Abuse the platform
Do not attempt to:
- Bypass or undermine our safety mechanisms.
- Jailbreak or prompt-inject the underlying models against their operator's policies.
- Extract model weights, system prompts, hidden rules, or training data via unauthorized means.
- Scrape, clone, train, fine-tune, or distill models the upstream provider has forbidden from being used that way.
- Exceed our published rate limits, quotas, or fair-use boundaries.
- Circumvent our billing, refund logic, or fraud controls (including creating multiple accounts to farm signup credits, referral credits, or free-trial slots).
- Manipulate the warranty logic to claim refills or refunds you would not otherwise be entitled to.

### 14. Generate sexually explicit content of real people
Do not generate explicit sexual content depicting real, identifiable people without their consent, or generate sexual content involving minors (see §7). Consenting adult content that is otherwise permitted by both the Vendor and the upstream model provider is subject to their policies, not ours.

## II. Additional rules for high-risk uses

If your application affects people's lives, livelihood, safety, or legal rights, you must do more than "don't break the rules." You must:

- **Keep a qualified human in the loop.** A person with the relevant professional qualification must review outputs before they are applied to someone's real-world decision.
- **Disclose AI at the start of each session** if the output is shown directly to an end user.

High-risk categories include:

- **Legal.** Legal interpretation, contract drafting, or advice that carries legal consequences.
- **Healthcare.** Medical diagnosis, medication advice, psychotherapy, or care decisions. General wellness content is not covered by this restriction.
- **Financial.** Investment advice, loan decisions, credit eligibility, insurance underwriting, or claims decisions.
- **Employment and housing.** Resume screening, hiring decisions, lease or mortgage eligibility.
- **Education assessment.** Standardized testing, admissions, or professional certification decisions.
- **News and media.** Auto-generating content for public publication under a real byline or brand.

## III. Other mandatory disclosures

- **Consumer-facing chat products** must inform users at the start of each session that they are interacting with an AI.
- **Products for minors** must comply with applicable child-protection laws (COPPA, GDPR-K, etc.) and implement age gating, content filtering, and parental controls.
- **Autonomous agents.** Requests executed by an autonomous agent are subject to this Policy the same as any other request. You, as the deploying party, bear final responsibility for what the agent does with your key.

## IV. Vendor-specific and model-specific policies

Kiro.bus routes to independent Vendors that route to independent model providers. Each of them has its own usage policy, and those policies pass through to you.

- Read the Vendor's terms and each model provider's usage policy before running production traffic through them.
- If a Vendor or model provider forbids a use case, you must not use their route for that purpose, even if that use case is not listed here.
- If a Vendor or model provider updates their policy, our best-effort default is to reflect that update; we do not warrant timeliness.

## V. Enforcement and appeals

Based on automated detection, human review, or a report from an upstream provider, we may:

- Block or filter individual requests.
- Rate-limit an account or an API key.
- Temporarily suspend or permanently ban an account.
- Withhold ongoing refunds or credits obtained by policy violations.
- Refer to law enforcement in cases of serious harm.

If you believe an action was taken in error, you may appeal through the [Community](/community) channels or by opening an issue on the GitHub repository. We review appeals within a reasonable timeframe and act on the merits.

## VI. Changes

We will continue to update this Policy as upstream policies and applicable laws evolve. Continued use of the Service after a new version takes effect constitutes acceptance.
