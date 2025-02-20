const React = require('react');

exports.Box = ({ children, alignItems, ...props }) =>
  React.createElement('div', { ...props, style: { alignItems } }, children);
exports.Inline = ({ children, ...props }) =>
  React.createElement('div', props, children);
