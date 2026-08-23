# Sending an email report

A report leaves the platform and lands in someone's inbox, where it cannot be
edited, recalled or explained. Everything below follows from that.

## The call

`send_email_report` posts to the orchestrator's `POST /email/send`:

```json
{"to": ["a@example.com"], "subject": "Deployment report — 3 failing", "html": "…", "text": "…"}
```

The sender identity and the provider key come from the installation's stored
email settings, never from you — a body that tries to supply either is refused.
If the answer says email is not configured, say exactly that: it is an operator
setting on **Admin → Email**, not something you can work around. Recipients are
capped and a newline in the subject is refused, so keep the subject one short
line.

`text` is optional and you should always send it anyway. It is what a plain-text
client, a screen reader and a notification preview show, and a report that
exists only as HTML reads as an empty message to all three.

## Before you send

- Send only when the person asked for an email. A report they can read in the
  panel does not need to be mailed to them as well.
- Say the recipients and the subject back to them, in the same turn, before the
  call. "Sending the deployment report to ada@example.com" is enough.
- Never mail a secret, an API key, an env binding's value, a bearer token, or a
  request body pulled out of a trace. A report names things and counts them.
- One address you were not given is one address too many: send to who they said,
  and to nobody else.

## HTML that survives a mail client

Mail clients are not browsers. Gmail strips `<style>` blocks on some clients,
Outlook renders through Word, and nothing supports flexbox, grid or `position`.
So:

- **Inline every style.** `style="…"` on the element, no stylesheet, no classes.
- **Lay out with tables.** `<table role="presentation" cellpadding="0"
  cellspacing="0" border="0">`, `width="100%"`, and a fixed inner width of
  600px. No divs for structure.
- **No JavaScript, no external CSS, no web fonts, no remote images.** They are
  blocked, and a layout that depends on one arrives broken.
- **Colour is decoration, never meaning.** Put the word "failed" next to the red,
  because a text client shows neither the colour nor the icon.
- **Font stack:** `-apple-system, "Segoe UI", Roboto, Helvetica, Arial,
  sans-serif`, 14–15px, `line-height: 1.5`, dark grey (`#1f2328`) on white.
- Dark mode inverts unpredictably: keep a light background and avoid near-black
  text on near-black panels.

### The skeleton

```html
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"
       style="background:#f5f6f7;padding:24px 0;">
  <tr><td align="center">
    <table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0"
           style="width:600px;max-width:100%;background:#ffffff;border:1px solid #e3e5e8;
                  border-radius:8px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;
                  color:#1f2328;font-size:14px;line-height:1.5;">
      <tr><td style="padding:20px 24px;border-bottom:1px solid #e3e5e8;">
        <div style="font-size:16px;font-weight:600;">Deployment report</div>
        <div style="color:#6b7280;font-size:12px;">octo platform · 2026-08-23 09:00 UTC</div>
      </td></tr>
      <tr><td style="padding:20px 24px;">
        <!-- summary sentence, then the table -->
      </td></tr>
      <tr><td style="padding:16px 24px;border-top:1px solid #e3e5e8;color:#6b7280;font-size:12px;">
        Sent by Dr. Octo. Reply to this address and a person will read it.
      </td></tr>
    </table>
  </td></tr>
</table>
```

### A data table inside it

```html
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"
       style="border-collapse:collapse;font-size:13px;">
  <tr style="background:#f5f6f7;">
    <th align="left" style="padding:8px 10px;border-bottom:1px solid #e3e5e8;">Integration</th>
    <th align="left" style="padding:8px 10px;border-bottom:1px solid #e3e5e8;">Status</th>
    <th align="right" style="padding:8px 10px;border-bottom:1px solid #e3e5e8;">Restarts</th>
  </tr>
  <tr>
    <td style="padding:8px 10px;border-bottom:1px solid #eef0f2;">orders-api</td>
    <td style="padding:8px 10px;border-bottom:1px solid #eef0f2;color:#b42318;">failed — CrashLoopBackOff</td>
    <td align="right" style="padding:8px 10px;border-bottom:1px solid #eef0f2;">14</td>
  </tr>
</table>
```

Status colours, used *with* the word: running `#067647`, degraded `#b54708`,
failed `#b42318`, neutral `#6b7280`.

## The shape of a report

Whatever it is about, the same five parts, in this order:

1. **A subject that is the finding.** "3 of 11 deployments failing" beats
   "Deployment report". Put the date in the header, not the subject.
2. **One sentence up top** that a person can act on without scrolling.
3. **The table** — the rows behind that sentence, worst first, capped at ~25 with
   "and 12 more" underneath rather than a 200-row email.
4. **What it does not cover.** A window with no data, an API that was not
   granted, a deployment that could not be read. State it; a silent gap reads as
   a clean bill of health.
5. **Where to look.** A platform URL per row where there is one.

## Reports worth sending, and what goes in each

| Report | Gather with | Rows are |
| --- | --- | --- |
| Deployment health | `GET /integrations`, then `GET /deployments/{id}` per deployment | deployment, status + reason, replicas ready, restarts |
| Failing runs digest | `GET /traces?status=failed&from=…` | flow, when, error, duration, trace id |
| Error log summary | `GET /logs?level=error&from=…` | deployment, count, the message, first and last seen |
| Integration inventory | `GET /integrations`, `GET /integrations/{id}/snapshots` | integration, latest tag, what is deployed, drift between the two |
| Change report after a rollout | the snapshot and rollout responses | what moved from which tag to which, by whom, and the status after |
| Storage and health | `GET /settings/storage`, `GET /settings/health` | store, used, capacity, what is unreachable |

A period always has an explicit window ("07:00–08:00 UTC, 23 Aug 2026") in the
header. "Last hour" in an inbox is ambiguous the moment it is read late.

## The text fallback

Not the HTML with the tags removed — a short plain version of the same thing:

```
3 of 11 deployments failing (23 Aug 2026, 09:00 UTC)

orders-api      failed — CrashLoopBackOff   restarts 14
billing-worker  failed — ImagePullBackOff   restarts 3
notifier        degraded — 1/2 ready        restarts 0

Not covered: 2 deployments could not be read (403).
```
