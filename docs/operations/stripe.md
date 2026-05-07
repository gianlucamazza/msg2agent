# Stripe Billing Operations

## 1. Setup

Configure the relay via environment variables before starting:

| Variable | Required | Description |
|---|---|---|
| `STRIPE_SECRET_KEY` | Yes | Stripe secret API key (`sk_live_…` or `sk_test_…`). Enables all billing endpoints. |
| `STRIPE_WEBHOOK_SECRET` | Yes | Signing secret for webhook signature verification (`whsec_…`). |
| `STRIPE_PRICE_STARTER` | Yes | Stripe Price ID for the Starter plan (`price_…`). |
| `STRIPE_PRICE_TEAM` | Yes | Stripe Price ID for the Team plan (`price_…`). |
| `STRIPE_PRICE_ENTERPRISE` | No | Stripe Price ID for the Enterprise plan (optional; Enterprise is typically custom). |

Example:

```sh
export STRIPE_SECRET_KEY=sk_live_...
export STRIPE_WEBHOOK_SECRET=whsec_...
export STRIPE_PRICE_STARTER=price_1ABC...
export STRIPE_PRICE_TEAM=price_1DEF...
```

## 2. Stripe Dashboard Configuration

### Webhook endpoint

In the Stripe Dashboard under **Developers → Webhooks**, add an endpoint:

```
https://${DOMAIN}/api/billing/webhook
```

### Events to enable

Select the following events to send to the webhook endpoint:

- `checkout.session.completed`
- `customer.subscription.updated`
- `customer.subscription.deleted`
- `invoice.payment_failed`

After creating the endpoint, copy the **Signing secret** (`whsec_…`) and set it as `STRIPE_WEBHOOK_SECRET`.

## 3. Onboarding Existing Customers

If a customer already exists in Stripe and needs to be linked to a local tenant:

```sh
billing-admin -db /path/to/billing.db attach-stripe \
  --tenant t_xxx \
  --customer cus_yyy
```

This sets `stripe_customer_id` on the tenant record without creating a new Stripe customer. The tenant can then use the Customer Portal to manage their subscription.

## 4. Recovery — Webhook Miss

If a webhook event was not received (e.g. downtime, misconfiguration), you can manually reconcile a tenant's subscription state from Stripe:

```sh
STRIPE_SECRET_KEY=sk_live_... billing-admin -db /path/to/billing.db sync-subscription \
  --tenant t_xxx
```

This fetches the subscription from Stripe, prints the diff, and updates the local tenant record.

To view current Stripe fields without modifying them:

```sh
billing-admin -db /path/to/billing.db list-stripe --tenant t_xxx
```

## 5. Webhook Signature Secret Rotation

To rotate the webhook signing secret without downtime:

1. In the Stripe Dashboard, create a new webhook endpoint (or reveal a new signing secret on the existing endpoint).
2. Update `STRIPE_WEBHOOK_SECRET` in your deployment configuration.
3. Perform a rolling restart of the relay so both old and new instances pick up the new secret.
4. Verify webhook delivery is working via the Stripe Dashboard event log.
5. Remove the old signing secret from the Dashboard once all relay instances have restarted.

> **Note:** Stripe signs each event with the current active secret. There is no overlap period on Stripe's side; update the relay before removing the old secret from the Dashboard.

## 6. Sandbox Testing

Use the [Stripe CLI](https://stripe.com/docs/stripe-cli) to forward test events to a local relay:

```sh
stripe listen --forward-to localhost:8080/api/billing/webhook
```

The CLI prints a webhook signing secret (`whsec_…`). Set it as `STRIPE_WEBHOOK_SECRET` for local testing:

```sh
export STRIPE_WEBHOOK_SECRET=whsec_test_...
```

To trigger a specific test event:

```sh
stripe trigger checkout.session.completed
stripe trigger customer.subscription.updated
stripe trigger customer.subscription.deleted
stripe trigger invoice.payment_failed
```

Use test mode keys (`sk_test_…`, `pk_test_…`) and test Price IDs when running against the Stripe test environment.

## 7. Price ID Mapping

| Plan | Environment Variable | Example Stripe Price ID |
|---|---|---|
| Starter ($19/mo) | `STRIPE_PRICE_STARTER` | `price_1ABC123…` |
| Team ($99/mo) | `STRIPE_PRICE_TEAM` | `price_1DEF456…` |
| Enterprise (custom) | `STRIPE_PRICE_ENTERPRISE` | `price_1GHI789…` |
| Free | — | Not applicable (no payment required) |

Price IDs are created in the Stripe Dashboard under **Products**. Create one Price per plan, set to **Recurring** with the appropriate billing interval (monthly).

## 8. Payment Failure Enforcement (`past_due`)

When `invoice.payment_failed` fires, the tenant's `BillingStatus` is set to `past_due`. From that point every API request returns:

```
HTTP 402 Payment Required
payment required: update your payment method at https://msg2agent.xyz/app/
```

Tenants resolve this by opening the Stripe Customer Portal (`GET /api/billing/portal`) and updating their payment method. Once Stripe retries and succeeds, `invoice.paid` / `customer.subscription.updated` flips `BillingStatus` back to `active`.

## 9. Plan Changes via Customer Portal

When a tenant upgrades or downgrades via the Stripe Customer Portal, `customer.subscription.updated` fires. The relay:

1. Reads `subscription.items.data[0].price.id` from the event.
2. Reverse-looks up the Price ID against the configured `STRIPE_PRICE_*` env vars to determine the new plan.
3. Updates `tenant.Plan` and resets `tenant.Quota` to the new plan's defaults.

If the Price ID is not recognised (e.g. a custom enterprise price), only `BillingStatus` is updated and `Plan` is left unchanged — reconcile manually via `billing-admin`.

## 10. Email Verification (SMTP)

Post-signup magic-link verification requires an SMTP relay. Configure via:

| Variable | Required | Description |
|---|---|---|
| `MSG2AGENT_SMTP_HOST` | Yes | SMTP server hostname (e.g. `smtp.sendgrid.net`). |
| `MSG2AGENT_SMTP_PORT` | No | SMTP port (default: `587`). |
| `MSG2AGENT_SMTP_USER` | Yes | SMTP username. |
| `MSG2AGENT_SMTP_PASS` | Yes | SMTP password / API key. |
| `MSG2AGENT_SMTP_FROM` | No | Sender address (default: `noreply@msg2agent.xyz`). |

If `MSG2AGENT_SMTP_HOST` is not set, email sending is silently skipped — signup continues normally but no verification link is sent.

Recommended providers with a free tier for low-volume sending: SendGrid (100/day), Mailgun, Postmark.

After configuring, verify SPF and DKIM records in your DNS for `msg2agent.xyz` to avoid deliverability issues.
