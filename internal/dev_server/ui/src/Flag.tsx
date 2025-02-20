import {
  Button,
  ButtonGroup,
  Dialog,
  DialogTrigger,
  FieldError,
  Form,
  IconButton,
  Label,
  ListBox,
  ListBoxItem,
  Modal,
  ModalOverlay,
  Popover,
  Select,
  SelectValue,
  Text,
  TextArea,
  TextField,
  Tooltip,
  TooltipTrigger,
} from '@launchpad-ui/components';
import { FormEvent, Fragment } from 'react';
import { Icon } from '@launchpad-ui/icons';
import { LDFlagValue } from 'launchdarkly-js-client-sdk';
import { FlagVariation } from './api.ts';
import { Box, Inline } from '@launchpad-ui/core';
import { isEqual } from 'lodash';
import { Switch } from 'react-aria-components';
import './Switch.css';

type VariationValuesProps = {
  availableVariations: FlagVariation[];
  currentValue: LDFlagValue;
  flagValue: LDFlagValue;
  flagKey: string;
  updateOverride: (flagKey: string, overrideValue: LDFlagValue) => void;
};

const VariationValues = ({
  availableVariations,
  currentValue,
  flagKey,
  flagValue,
  updateOverride,
}: VariationValuesProps) => {
  switch (typeof flagValue) {
    case 'boolean':
      return (
        <div className="animated-switch-container">
          <Switch
            className="animated-switch"
            isSelected={currentValue}
            onChange={(newValue) => {
              updateOverride(flagKey, newValue);
            }}
          >
            <span className="switch-text switch-text-false">False</span>
            <span className="switch-text switch-text-true">True</span>
          </Switch>
        </div>
      );
    default: {
      let variations = availableVariations;
      let selectedVariationIndex = variations.findIndex((variation) =>
        isEqual(variation.value, currentValue),
      );
      if (selectedVariationIndex === -1) {
        variations = [
          { _id: 'OVERRIDE', name: 'Local Override', value: currentValue },
          ...variations,
        ];
        selectedVariationIndex = 0;
      }
      const onSubmit = (close: () => void) => (e: FormEvent<HTMLFormElement>) => {
        // Prevent default browser page refresh.
        e.preventDefault();
        const data = Object.fromEntries(new FormData(e.currentTarget));
        updateOverride(flagKey, JSON.parse(data.value as string));
        close();
      };


      //TODO:
      // Grow the text area when editing local override
      return (
        <Inline gap="2">
          <Select
            aria-label="flag variations select"
            selectedKey={selectedVariationIndex}
            onSelectionChange={(key) => {
              if (typeof key != 'number') {
                console.error(`selected non numeric key: ${key}`);
              } else {
                updateOverride(flagKey, variations[key].value);
              }
            }}
            style={{
              maxWidth: '250px',
            }}
          >
            <Fragment key=".0">
              {selectedVariationIndex !== null &&
              variations[selectedVariationIndex]._id === 'OVERRIDE' ? (
                <TooltipTrigger>
                  <Button>
                    <SelectValue />
                    <Icon name="chevron-down" size="small" />
                  </Button>
                  <Tooltip>
                    This value is overriden locally. Click the edit button to
                    change the value served.
                  </Tooltip>
                </TooltipTrigger>
              ) : (
                <Button>
                  <SelectValue
                    style={{
                      maxWidth: '250px',
                      whiteSpace: 'nowrap',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  />
                  <Icon name="chevron-down" size="small" />
                </Button>
              )}
              <Popover>
                <ListBox>
                  {variations.map((fv, index) => {
                    const text = fv.name ? fv.name : JSON.stringify(fv.value);
                    return (
                      <ListBoxItem key={index} id={index} textValue={text}>
                        {fv._id === 'OVERRIDE' ? (
                          <div>
                            <Inline gap="1">
                              <Icon name="devices" size="small" />
                              {text}
                            </Inline>
                          </div>
                        ) : (
                          text
                        )}
                      </ListBoxItem>
                    );
                  })}
                </ListBox>
              </Popover>
            </Fragment>
          </Select>
          <Box width="2rem" height="2rem">
            <DialogTrigger>
              <TooltipTrigger>
                <IconButton icon="edit" aria-label="edit variation value" />
                <Tooltip>Edit the served variation value as JSON</Tooltip>
              </TooltipTrigger>
              <ModalOverlay>
                <Modal>
                  <Dialog>
                    {({ close }) => (
                      <Form onSubmit={onSubmit(close)}>
                        <TextField
                          defaultValue={JSON.stringify(currentValue, null, 2)}
                          name="value"
                          style={{
                            fontFamily: 'monospace',
                            height: '25rem',
                          }}
                          validate={(value) => {
                            try {
                              JSON.parse(value);
                              return null;
                            } catch (err) {
                              if (err instanceof Error) {
                                return `Unable to parse value as JSON: ${err.toString()}`;
                              } else {
                                return `Unable to parse value as JSON: unknown parse error`;
                              }
                            }
                          }}
                        >
                          <Label>{`${flagKey} value`}</Label>
                          <TextArea
                            style={{
                              fontFamily: 'monospace',
                            }}
                          />
                          <Text slot="description">
                            Update the value as JSON
                          </Text>
                          <FieldError />
                        </TextField>
                        <ButtonGroup>
                          <Button onPress={close}>Cancel</Button>
                          <Button variant="primary" type="submit">
                            Save
                          </Button>
                        </ButtonGroup>
                      </Form>
                    )}
                  </Dialog>
                </Modal>
              </ModalOverlay>
            </DialogTrigger>
          </Box>
        </Inline>
      );
    }
  }
};

export default VariationValues;
