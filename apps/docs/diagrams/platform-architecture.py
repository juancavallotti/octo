#!/usr/bin/env python3
"""The platform component map, replacing the ASCII block in platform/architecture.mdx."""
import random
from build import box, cylinder, arrow, write

random.seed(7)

E = []
E += box("browser", 240, 0, 220, 60, "you (browser)")
E += arrow(350, 60, 350, 130, "HTTPS, through the ingress", frm="browser", to="platform")

E += box("platform", 150, 130, 400, 90,
         "Platform app  :3000\nNext.js editor + BFF\nAuth.js OIDC session, server proxy")
E += arrow(350, 220, 350, 290, "HTTP, server side only", frm="platform", to="orchestrator")

E += box("orchestrator", 120, 290, 460, 110,
         "Orchestrator (Go)  :8090\nintegrations, deployments, KV\nsnapshots, secrets, users, keys")
E += arrow(580, 330, 740, 330, "applies manifests", frm="orchestrator", to="kube")
E += box("kube", 740, 300, 220, 60, "Kubernetes API")
E += arrow(850, 360, 850, 470, "creates", frm="kube", to="pods")

E += box("pods", 700, 470, 300, 100,
         "Runtime pods (data plane)\none Deployment per\ndeployed integration")
E += arrow(700, 520, 520, 520, "telemetry and queues", frm="pods", to="nats")

E += arrow(220, 400, 180, 470, "SQL", frm="orchestrator", to="postgres")
E += arrow(450, 400, 470, 470, "publishes", frm="orchestrator", to="nats")
E += cylinder("postgres", 60, 470, 240, 100, "Postgres  :5432")
E += box("nats", 360, 470, 200, 100, "NATS  :4222")

E += arrow(460, 570, 460, 660, "internal.logs, internal.traces", frm="nats", to="obs")
E += box("obs", 300, 660, 320, 80, "Observability service\n:8091")
E += arrow(300, 700, 180, 570, "SQL", frm="obs", to="postgres")

write("platform-architecture.excalidraw", E)
