import { Select, SelectValue, Button, Popover, ListBox, ListBoxItem } from '@launchpad-ui/components';
import { Icon } from '@launchpad-ui/icons';
import { useNavigate, useLocation } from 'react-router';
import { Fragment } from 'react';

const RouteSelector = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const options = [
    { key: '/ui/flags', label: 'Flags' },
    { key: '/ui/events', label: 'Events' }
  ];

  const currentPath = location.pathname === '/' ? '/ui' : location.pathname;
  const currentOption = options.find(option => option.key === currentPath);

  const handleSelectionChange = (key: React.Key) => {
    if (typeof key === 'string') {
      navigate(key);
    }
  };

  return (
    <Select
      aria-label="Route selector"
      selectedKey={currentPath}
      onSelectionChange={handleSelectionChange}
      style={{ minWidth: '150px' }}
    >
      <Fragment>
        <Button>
          <SelectValue>
            {currentOption?.label || 'Select a view'}
          </SelectValue>
          <Icon name="chevron-down" size="small" />
        </Button>
        <Popover>
          <ListBox>
            {options.map((option) => (
              <ListBoxItem key={option.key} id={option.key} textValue={option.label}>
                {option.label}
              </ListBoxItem>
            ))}
          </ListBox>
        </Popover>
      </Fragment>
    </Select>
  );
};

export default RouteSelector;
