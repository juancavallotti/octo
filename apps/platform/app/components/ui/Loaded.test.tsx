import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import Loaded from "./Loaded";

const skeleton = <p>skeleton</p>;

it("shows the skeleton while loading, and marks the region busy", () => {
  const { container } = render(
    <Loaded state={{ status: "loading" }} skeleton={skeleton}>
      {() => <p>content</p>}
    </Loaded>,
  );
  expect(screen.getByText("skeleton")).toBeInTheDocument();
  expect(screen.queryByText("content")).not.toBeInTheDocument();
  expect(container.firstChild).toHaveAttribute("aria-busy", "true");
});

it("shows why a load failed instead of the content", () => {
  render(
    <Loaded state={{ status: "failed", error: "no orchestrator" }} skeleton={skeleton}>
      {() => <p>content</p>}
    </Loaded>,
  );
  expect(screen.getByText("no orchestrator")).toBeInTheDocument();
  expect(screen.queryByText("skeleton")).not.toBeInTheDocument();
});

it("hands the data to the children once ready, and keeps it while refreshing", () => {
  render(
    <Loaded
      state={{ status: "ready", data: ["a"], refreshing: true, error: null }}
      skeleton={skeleton}
    >
      {(data, refreshing) => <p>{`${data.join()} ${refreshing ? "refreshing" : ""}`}</p>}
    </Loaded>,
  );
  expect(screen.getByText("a refreshing")).toBeInTheDocument();
  expect(screen.queryByText("skeleton")).not.toBeInTheDocument();
});
