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
  Dialog,
  TextArea,
} from '@launchpad-ui/components';
import {
  Box,
  CopyToClipboard,
  InlineEdit,
  TextField,
} from '@launchpad-ui/core';
import Theme from '@launchpad-ui/tokens';
import { useEffect, useRef, useState } from 'react';
import { Icon } from '@launchpad-ui/icons';
import { apiRoute, sortFlags } from './util.ts';

type FlagProps = {
  selectedProject: string;
  flags: LDFlagSet | null;
  setFlags: (flags: LDFlagSet) => void;
};

function Flags({ selectedProject, flags, setFlags }: FlagProps) {
  const [overrides, setOverrides] = useState<Record<
    string,
    { value: LDFlagValue }
  > | null>(null);
  const [onlyShowOverrides, setOnlyShowOverrides] = useState(false);
  const overridesPresent = overrides && Object.keys(overrides).length > 0;
  const textAreaRef = useRef<HTMLTextAreaElement>(null);

  const updateOverride = (flagKey: string, overrideValue: LDFlagValue) => {
    fetch(apiRoute(`/dev/projects/${selectedProject}/overrides/${flagKey}`), {
      method: 'PUT',
      body: JSON.stringify(overrideValue),
    })
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(`got ${res.status} ${res.statusText}. ${await res.text()}`)
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
      .catch( console.error.bind(console, "unable to update override"));
  };

  const removeOverride = (flagKey: string, updateState: boolean = true) => {
    return fetch(
      apiRoute(`/dev/projects/${selectedProject}/overrides/${flagKey}`),
      {
        method: 'DELETE',
      },
    )
      .then((res) => {
        // In the remove-all-override case, we need to fan out and make the
        // request for every override, so we don't want to be interleaving
        // local state updates. Expect the consumer to update the local state
        // when all requests are done
        if (res.ok && updateState) {
          const updatedOverrides = { ...overrides };
          delete updatedOverrides[flagKey];

          setOverrides(updatedOverrides);

          if (Object.keys(updatedOverrides).length === 0)
            setOnlyShowOverrides(false);
        }
      })
      .catch( console.error.bind("unable to remove override") );
  };

  const fetchFlags = async () => {
    const res = await fetch(
      apiRoute(`/dev/projects/${selectedProject}?expand=overrides`),
    );
    const json = await res.json();
    if (!res.ok) {
      throw new Error(`Got ${res.status}, ${res.statusText} from flag fetch`);
    }

    const { flagsState: flags, overrides } = json;

    setFlags(sortFlags(flags));
    setOverrides(overrides);
  };

  // Fetch flags / overrides on mount
  useEffect(() => {
    fetchFlags().catch(
      console.error.bind(console, 'error when fetching flags'),
    );
  }, [selectedProject]);

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
          {Object.entries(flags).map(
            ([flagKey, { value: flagValue }], index) => {
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
                    <TextField
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
                      renderInput={
                        <TextField id={`${flagKey}-override-input`} />
                      }
                    >
                      {hasOverride ? overrideValue : flagValue}
                    </InlineEdit>
                  );
                  break;
                default:
                  valueNode = (
                    <DialogTrigger>
                      <Button style={{ border: 'none', padding: 0, margin: 0 }}>
                        <TextArea
                          rows={8}
                          readOnly={true}
                          style={{
                            resize: 'none',
                            overflowY: 'clip',
                            cursor: 'pointer',
                          }}
                          value={JSON.stringify(
                            hasOverride ? overrideValue : flagValue,
                            null,
                            2,
                          )}
                        ></TextArea>
                      </Button>
                      <ModalOverlay>
                        <Modal>
                          <Dialog>
                            {({ close }) => (
                              <form
                                onSubmit={() => {
                                  let newVal;

                                  try {
                                    newVal = JSON.parse(
                                      textAreaRef?.current?.value || '',
                                    );
                                  } catch (err) {
                                    window.alert('Invalid JSON formatting');
                                    return;
                                  }

                                  updateOverride(flagKey, newVal);
                                }}
                              >
                                <TextArea
                                  ref={textAreaRef}
                                  style={{ width: '100%', height: '30rem' }}
                                  defaultValue={JSON.stringify(
                                    hasOverride ? overrideValue : flagValue,
                                    null,
                                    2,
                                  )}
                                />
                                <div>
                                  <Button
                                    variant="primary"
                                    type="submit"
                                    onPress={close}
                                  >
                                    Accept
                                  </Button>
                                </div>
                              </form>
                            )}
                          </Dialog>
                        </Modal>
                      </ModalOverlay>
                    </DialogTrigger>
                  );
              }

              return (
                <li
                  key={flagKey}
                  style={{
                    backgroundColor: index % 2 === 0 ? 'white' : '#f8f8f8',
                  }}
                >
                  <Box
                    whiteSpace="nowrap"
                    flexGrow="1"
                    paddingLeft="1rem"
                    paddingRight="1rem"
                  >
                    <CopyToClipboard asChild text={flagKey}>
                      <code className={hasOverride ? 'has-override' : ''}>
                        {flagKey}
                      </code>
                    </CopyToClipboard>
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
            },
          )}
        </ul>
      </div>
    </>
  );
}

export default Flags;
