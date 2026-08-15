# {{ body.city }}

A briefing for **{{ body.city }}**, served through a URI template rather than a
fixed resource: one declaration stands for every city a client might ask about.

The value the client put in the `{city}` slot arrives as `body.city`, the same
way a prompt's arguments do. A resource template also sees the request's
variables, so a router behind a `jwt-validate` can render per-caller content
without any extra wiring.
