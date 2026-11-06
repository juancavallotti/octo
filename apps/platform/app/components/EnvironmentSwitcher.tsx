"use client";

import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import { useEnvironment } from "@/app/auth/EnvironmentContext";
import { listEnvironments, selectEnvironment } from "@/app/model/environments";
import EnvironmentMenu from "./EnvironmentMenu";
import {
  landingAfterSwitch,
  reloadAt,
  visibleEnvironments,
  type EnvironmentOption,
} from "./environmentSwitch";

/**
 * Which environment every screen is showing, and the control to change it.
 *
 * Shown only when there is a choice: a person who works in one environment
 * sees nothing new. Switching writes the choice server-side and reloads, so no
 * page keeps showing what it loaded from the environment it just left.
 *
 * A chosen environment that has since been deleted is put back to the default
 * as soon as the list shows it gone, rather than leaving every call refused.
 */
export default function EnvironmentSwitcher() {
  const context = useEnvironment();
  const pathname = usePathname();
  const [options, setOptions] = useState<EnvironmentOption[] | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!context) return;
    let live = true;
    listEnvironments().then(
      (all) => {
        if (!live) return;
        const current = context.environment;
        if (current !== "" && !all.some((e) => e.name === current)) {
          selectEnvironment("").then(() => reloadAt(landingAfterSwitch(pathname)));
          return;
        }
        setOptions(visibleEnvironments(all, context.roles, context.envRoles));
      },
      () => live && setOptions([]),
    );
    return () => {
      live = false;
    };
  }, [context, pathname]);

  if (!context || options === null) return null;
  const current = context.environment;
  if (options.length <= 1 && current === "") return null;

  const choose = async (name: string) => {
    setBusy(true);
    try {
      await selectEnvironment(name);
      reloadAt(landingAfterSwitch(pathname));
    } catch {
      setBusy(false);
    }
  };

  return (
    <EnvironmentMenu
      options={options}
      current={current}
      busy={busy}
      manageHref={context.roles.includes("platform:admin") ? "/platform/admin/environments" : undefined}
      onChoose={choose}
    />
  );
}
