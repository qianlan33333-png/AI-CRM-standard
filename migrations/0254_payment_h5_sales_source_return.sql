-- Owner: Payment. Preserve the entire 0228 return allowlist and existing
-- states; add only the sales-source modifier already accepted by H5 OAuth.
ALTER TABLE payment_h5_oauth_states
  DROP CONSTRAINT payment_h5_oauth_states_return_path_check;
ALTER TABLE payment_h5_oauth_states
  ADD CONSTRAINT payment_h5_oauth_states_return_path_check CHECK (
    (return_path ~ '^/(pay/[^/?#]+|s/[^/?#]+/pay)[?]resume_order=[1-9][0-9]*$'
      AND return_path !~ '%' AND position(E'\\' in return_path)=0
      AND CASE WHEN return_path ~ '^/(pay/[^/?#]+|s/[^/?#]+/pay)[?]resume_order=[1-9][0-9]*$' THEN
        (regexp_match(return_path, '[?]resume_order=([1-9][0-9]*)$'))[1]::numeric <= 9223372036854775807
      ELSE FALSE END)
    OR return_path = '/my/orders'
    OR (
      return_path ~ '^/(p/[^/?#]+([?]promotion_context=dpc_[A-Za-z0-9_-]{43})?|pay/[^/?#]+([?]promotion_context=dpc_[A-Za-z0-9_-]{43})?|s/[^/?#]+(/pay)?([?]promotion_context=dpc_[A-Za-z0-9_-]{43})?|c/[a-z][a-z0-9-]{5,119}|distribution([?]product_id=[1-9][0-9]*&product_type=(standard_product|service_period))?|referral([?]campaign=[1-9][0-9]*(&invite=rfi_[A-Za-z0-9_-]{43})?)?)$'
      AND return_path !~ '%(2[fF]|5[cC]|3[fF]|23)'
      AND position(E'\\' in return_path)=0
      AND CASE WHEN return_path ~ '[?]promotion_context=' THEN return_path !~ '%' ELSE TRUE END
      AND CASE
        WHEN return_path ~ '^/distribution[?]' THEN
          (regexp_match(return_path, '^/distribution[?]product_id=([1-9][0-9]*)&product_type=(standard_product|service_period)$'))[1]::numeric <= 9223372036854775807
        WHEN return_path ~ '^/referral[?]' THEN
          (regexp_match(return_path, '^/referral[?]campaign=([1-9][0-9]*)(?:&invite=rfi_[A-Za-z0-9_-]{43})?$'))[1]::numeric <= 9223372036854775807
        ELSE TRUE
      END
    )
    OR (
      -- Only commerce pages accept a single canonical source, optionally
      -- beside the existing promotion OR original-order query. Neither is
      -- a general next URL. Both query orders match the application policy.
      (
        return_path ~ '^/(p/[^/?#]+|pay/[^/?#]+|s/[^/?#]+(/pay)?)[?](sales_source=(unrecorded|[1-9][0-9]*)(&promotion_context=dpc_[A-Za-z0-9_-]{43})?|promotion_context=dpc_[A-Za-z0-9_-]{43}&sales_source=(unrecorded|[1-9][0-9]*))$'
        OR return_path ~ '^/(pay/[^/?#]+|s/[^/?#]+/pay)[?](sales_source=(unrecorded|[1-9][0-9]*)&resume_order=[1-9][0-9]*|resume_order=[1-9][0-9]*&sales_source=(unrecorded|[1-9][0-9]*))$'
      )
      AND return_path !~ '%(2[fF]|5[cC]|3[fF]|23)'
      AND position(E'\\' in return_path)=0
      AND return_path !~ '[[:cntrl:]]'
      AND CASE WHEN return_path ~ '[?&](promotion_context|resume_order)=' THEN return_path !~ '%' ELSE TRUE END
      AND CASE WHEN return_path ~ '[?&]sales_source=[1-9][0-9]*(&|$)' THEN
        (regexp_match(return_path, '[?&]sales_source=([1-9][0-9]*)(&|$)'))[1]::numeric <= 9223372036854775807
      ELSE return_path ~ '[?&]sales_source=unrecorded(&|$)' END
      AND CASE WHEN return_path ~ '[?&]resume_order=[1-9][0-9]*(&|$)' THEN
        (regexp_match(return_path, '[?&]resume_order=([1-9][0-9]*)(&|$)'))[1]::numeric <= 9223372036854775807
      ELSE TRUE END
    )
  );
