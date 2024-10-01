const React = require('react');

exports.Button = ({ children, isDisabled, ...props }) => React.createElement('button', { ...props, disabled: isDisabled }, children);
exports.ProgressBar = ({ isIndeterminate, ...props }) => React.createElement('div', { ...props, 'data-indeterminate': isIndeterminate }, null);
