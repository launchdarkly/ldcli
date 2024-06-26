import { LDFlagSet, LDFlagValue } from "launchdarkly-js-client-sdk";
import { Switch, TextField } from "@launchpad-ui/components";
import { CopyToClipboard, InlineEdit } from "@launchpad-ui/core";
import "./App.css";

function App() {
  // TODO: replace these with actual flags when we wire up the APIs
  const mockFlags: LDFlagSet = {
    "flag-b": 10,
    "flag-a": true,
    "flag-c": {
      propA: "A",
    },
  };

  const sortedFlags = Object.keys(mockFlags)
    .sort((a, b) => a.localeCompare(b))
    .reduce<Record<string, LDFlagValue>>((accum, flagKey) => {
      accum[flagKey] = mockFlags[flagKey];

      return accum;
    }, {});

  return (
    <>
      <div>
        <ul className="flags-list">
          {Object.entries(sortedFlags).map(([flagKey, flagValue]) => {
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
                <span>{flagKey}</span>
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
