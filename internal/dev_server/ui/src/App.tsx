import { LDFlagSet, LDFlagValue } from "launchdarkly-js-client-sdk";
import { Switch } from "@launchpad-ui/components";
import { CopyToClipboard, InlineEdit, TextField } from "@launchpad-ui/core";
import "./App.css";
import { useEffect, useState } from "react";

function App() {
  const [flags, setFlags] = useState<LDFlagSet | null>(null);

  useEffect(() => {
    fetch("/api/dev/projects/default?expand=overrides")
      .then(async (res) => {
        if (res.ok) {
          const flags = (await res.json()).flagsState;
          const sortedFlags = Object.keys(flags)
            .sort((a, b) => a.localeCompare(b))
            .reduce<Record<string, LDFlagValue>>((accum, flagKey) => {
              accum[flagKey] = flags[flagKey];

              return accum;
            }, {});

          setFlags(sortedFlags);
        }
      })
      .catch((e) => {
        // todo: handle failure to load flag data
      });
  }, []);

  return (
    <>
      <div>
        <ul className="flags-list">
          {flags &&
            Object.entries(flags).map(([flagKey, { value: flagValue }]) => {
              let valueNode;

              if (typeof flagValue === "boolean") {
                valueNode = (
                  <Switch
                    isSelected={flagValue}
                    onChange={(newValue) => {
                      // todo
                    }}
                  />
                );
              } else if (typeof flagValue === "number") {
                valueNode = (
                  <input
                    type="number"
                    value={flagValue}
                    onChange={(e) => {
                      // todo
                    }}
                  />
                );
              } else {
                valueNode = (
                  <InlineEdit
                    defaultValue={JSON.stringify(flagValue)}
                    onConfirm={(newValue: string) => {
                      // todo
                    }}
                    renderInput={<TextField id={`${flagKey}-override-input`} />}
                  >
                    <CopyToClipboard
                      text={JSON.stringify(flagValue)}
                      tooltip="Copy flag variation value"
                    >
                      {JSON.stringify(flagValue)}
                    </CopyToClipboard>
                  </InlineEdit>
                );
              }

              return (
                <li key={flagKey}>
                  <span>
                    <code>{flagKey}</code>
                  </span>
                  <div>{valueNode}</div>
                </li>
              );
            })}
        </ul>
      </div>
    </>
  );
}

export default App;
