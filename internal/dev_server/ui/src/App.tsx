import { LDFlagSet, LDFlagValue } from 'launchdarkly-js-client-sdk';
import {
  Button,
  Checkbox,
  IconButton,
  Label,
  Switch,
  Modal,
  ModalOverlay,
  DialogTrigger,
  Dialog
} from '@launchpad-ui/components';
import {
  Box,
  InlineEdit,
  TextField,
} from '@launchpad-ui/core';
import Theme from '@launchpad-ui/tokens';
import './App.css';
import { FormEventHandler, useEffect, useState } from 'react';
import { Icon } from '@launchpad-ui/icons';

function App() {
  const [flags, setFlags] = useState<LDFlagSet | null>(null);
  const [overrides, setOverrides] = useState<Record<
    string,
    { value: LDFlagValue }
  > | null>(null);
  const [onlyShowOverrides, setOnlyShowOverrides] = useState(false);
  const overridesPresent = overrides && Object.keys(overrides).length > 0;

  const updateOverride = (flagKey: string, overrideValue: LDFlagValue) => {
    fetch(`api/dev/projects/default/overrides/${flagKey}`, {
      method: 'PUT',
      body: JSON.stringify(overrideValue),
    })
      .then(async (res) => {
        if (!res.ok) {
          return; // todo
        }

        const updatedOverrides = {
          ...overrides,
          ...{
            [flagKey]: {
              value: overrideValue,
            },
          },
        };

        setOverrides(updatedOverrides);
      })
      .catch((e) => {
        // todo
      });
  };

  const removeOverride = (flagKey: string, updateState: boolean = true) => {
    return fetch(`api/dev/projects/default/overrides/${flagKey}`, {
      method: 'DELETE',
    })
      .then((res) => {
        // todo: clean this up.
        //
        // In the remove-all-override case, we need to fan out and make the
        // request for every override, so we don't want to be interleaving
        // local state updates. Expect the consumer to update the local state
        // when all requests are done
        if (res.ok && updateState) {
          const updatedOverrides = { ...overrides };
          delete updatedOverrides[flagKey];

          setOverrides(updatedOverrides);
        }
      })
      .catch((e) => {
        // todo
      });
  };

  // const updateJSON = (e, key) => {
  //   e.preventDefault()
  //   console.log(e.target[0].value)
  //   console.log(key)
  // }

  // Fetch flags / overrides on mount
  useEffect(() => {
    fetch('/api/dev/projects/default?expand=overrides')
      .then(async (res) => {
        if (!res.ok) {
          return; // todo
        }

        const json = await res.json();
        const { flagsState: flags, overrides } = json;
        const sortedFlags = Object.keys(flags)
          .sort((a, b) => a.localeCompare(b))
          .reduce<Record<string, LDFlagValue>>((accum, flagKey) => {
            accum[flagKey] = flags[flagKey];

            return accum;
          }, {});

        setFlags(sortedFlags);
        setOverrides(overrides);
      })
      .catch((e) => {
        // todo
      });
  }, []);

  if (!flags) {
    return null;
  }

  return (
    <>
      <div className="container">
        <Box
          display="flex"
          flexDirection="row"
          justifyContent="space-between"
          alignItems="center"
          marginBottom="2rem"
          padding="1rem"
          background={Theme.color.blue[50]}
          borderRadius={Theme.borderRadius.regular}
        >
          <Label
            htmlFor="only-show-overrides"
            className="only-show-overrides-label"
          >
            <Checkbox
              id="only-show-overrides"
              isSelected={onlyShowOverrides}
              onChange={(newValue) => {
                setOnlyShowOverrides(newValue);
              }}
              isDisabled={!overridesPresent}
              style={{
                display: 'inline-block',
                marginRight: '.25rem',
              }}
            />
            Only show flags with overrides
          </Label>
          <Button
            variant="destructive"
            isDisabled={!overridesPresent}
            onPress={async () => {
              // This button is disabled unless overrides are present, but the
              // type is nullable
              if (!overrides) {
                return;
              }

              const overrideKeys = Object.keys(overrides);

              await Promise.all(
                overrideKeys.map((flagKey) => {
                  // Opt out of local state updates since we're bulk-removing
                  // overrides async
                  removeOverride(flagKey, false);
                }),
              );

              // Winnow out removed overrides and update local state in a
              // single pass
              const updatedOverrides = overrideKeys.reduce(
                (accum, flagKey) => {
                  delete accum[flagKey];

                  return accum;
                },
                { ...overrides },
              );

              setOverrides(updatedOverrides);
              setOnlyShowOverrides(false);
            }}
          >
            <Icon size="medium" name="cancel" />
            Remove all overrides
          </Button>
        </Box>
        <ul className="flags-list">
          {Object.entries(flags).map(([flagKey, { value: flagValue }]) => {
            const overrideValue = overrides?.[flagKey]?.value;
            const hasOverride = overrideValue !== undefined;
            let valueNode;

            if (onlyShowOverrides && !hasOverride) {
              return null;
            }

            switch (typeof flagValue) {
              case 'boolean':
                valueNode = (
                  <Switch
                    isSelected={hasOverride ? overrideValue : flagValue}
                    onChange={(newValue) => {
                      updateOverride(flagKey, newValue);
                    }}
                  />
                );
                break;
              case 'number':
                valueNode = (
                  <input
                    type="number"
                    value={hasOverride ? Number(overrideValue) : flagValue}
                    onChange={(e) => {
                      updateOverride(flagKey, Number(e.target.value));
                    }}
                  />
                );
                break;
              case 'string':
                valueNode = (
                  <InlineEdit
                    defaultValue={hasOverride ? overrideValue : flagValue}
                    onConfirm={(newValue: string) => {
                      updateOverride(flagKey, newValue);
                    }}
                    renderInput={<TextField id={`${flagKey}-override-input`} />}
                  >
                    {hasOverride ? overrideValue : flagValue}
                  </InlineEdit>
                );
                break;
              default:
                valueNode = (
                  <DialogTrigger >
                    <Button style={{ border: 'none', padding: 0, margin: 0 }}>
                      <textarea name='json' rows={8} readOnly={true} style={{ resize: 'none', overflowY: 'clip', cursor: 'pointer' }}
                        value={JSON.stringify((hasOverride ? overrideValue : flagValue), null, 2)}>
                      </textarea>
                    </Button>
                    <ModalOverlay>
                      <Modal>
                        <Dialog >
                          <form onSubmit={(e: any) => {
                            e.preventDefault();
                            let newVal
                            let error = false; 
                            try {
                              newVal = JSON.parse(e.target[0].value);
                              }
                              catch (err) {
                                error = true
                                }
                            if (error) {
                              window.alert("Incorrect JSON format")
                              return;
                            }
                            updateOverride(flagKey, newVal)
                            window.alert('JSON value updated')
                          }}>
                            <textarea name='json' style={{ width: '100%', height: '30rem' }}
                              defaultValue={JSON.stringify((hasOverride ? overrideValue : flagValue), null, 2)} />

                            <div>
                              <Button variant='primary' type='submit'>
                                Accept
                              </Button>
                            </div>
                          </form>
                        </Dialog>
                      </Modal>
                    </ModalOverlay>
                  </DialogTrigger>
                );
            }

            return (
              <li key={flagKey}>
                <Box whiteSpace="nowrap" flexGrow="1" paddingRight="1rem">
                  <code className={hasOverride ? 'has-override' : ''}>
                    {flagKey}
                  </code>
                </Box>
                <div className="flag-value">{valueNode}</div>
                <Box width="2rem" height="2rem" marginLeft="0.5rem">
                  {hasOverride && (
                    <IconButton
                      icon="cancel"
                      aria-label="Remove override"
                      onPress={() => {
                        removeOverride(flagKey);
                      }}
                      variant="destructive"
                    />
                  )}
                </Box>
              </li>
            );
          })}
        </ul>
      </div>
    </>
  );
}

export default App;
