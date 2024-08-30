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
  Switch,
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
import { Box } from '@launchpad-ui/core';

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
        <Switch
          isSelected={currentValue}
          onChange={(newValue) => {
            updateOverride(flagKey, newValue);
          }}
        />
      );
    default:
      let variations = availableVariations;
      let selectedVariationIndex = variations.findIndex(
        (variation) => variation.value === currentValue,
      );
      if (selectedVariationIndex === -1) {
        variations = [
          { _id: 'OVERRIDE', name: 'Local Override', value: currentValue },
          ...variations,
        ];
        selectedVariationIndex = 0;
      }
      let onSubmit = (close: () => void) => (e: FormEvent<HTMLFormElement>) => {
        // Prevent default browser page refresh.
        e.preventDefault();
        let data = Object.fromEntries(new FormData(e.currentTarget));
        updateOverride(flagKey, JSON.parse(data.value as string));
        close();
      };

      //TODO:
      // Popover for edit button to explain
      // content in the edit modal
      return (
        <>
          <Box width="2rem" height="2rem" marginRight="0.5rem">
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
                          defaultValue={JSON.stringify(currentValue)}
                          name="value"
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
                          <TextArea />
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
          >
            <Fragment key=".0">
              <Button>
                <SelectValue />
                <Icon name="chevron-down" size="small" />
              </Button>
              <Popover>
                <ListBox>
                  {variations.map((fv, index) => {
                    const text = fv.name ? fv.name : JSON.stringify(fv.value);
                    return (
                      <ListBoxItem key={index} id={index} textValue={text}>
                        {fv._id === 'OVERRIDE' ? (
                          <>
                            {text}
                            <Icon name="devices" size="small" />
                          </>
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
        </>
      );
  }
};

export default VariationValues;
