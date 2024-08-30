import {
  Button,
  ListBox,
  ListBoxItem,
  Popover,
  Select,
  SelectValue,
  Switch,
} from '@launchpad-ui/components';
import { Fragment } from 'react';
import { Icon } from '@launchpad-ui/icons';
import { LDFlagValue } from 'launchdarkly-js-client-sdk';
import { FlagVariation } from './api.ts';

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
          { _id: 'OVERRIDE', name: 'local override', value: currentValue },
          ...variations,
        ];
        selectedVariationIndex = 0;
      }

      return (
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
                {variations.map((fv, index) => (
                  <ListBoxItem key={index} id={index}>
                    {fv.name ? fv.name : JSON.stringify(fv.value)}
                  </ListBoxItem>
                ))}
              </ListBox>
            </Popover>
          </Fragment>
        </Select>
      );
  }
};

export default VariationValues;
