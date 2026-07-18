// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

import { useRouter } from 'next/router';
import { useCallback, useState } from 'react';
import CartItems from '../CartItems';
import CheckoutForm from '../CheckoutForm';
import { IFormData } from '../CheckoutForm/CheckoutForm';
import SessionGateway from '../../gateways/Session.gateway';
import { useCart } from '../../providers/Cart.provider';
import { useCurrency } from '../../providers/Currency.provider';
import * as S from '../../styles/Cart.styled';

const { userId } = SessionGateway.getSession();

const CartDetail = () => {
  const {
    cart: { items },
    emptyCart,
    placeOrder,
  } = useCart();
  const { selectedCurrency } = useCurrency();
  const { push } = useRouter();
  const [orderError, setOrderError] = useState<string>();

  const onPlaceOrder = useCallback(
    async ({
      email,
      state,
      streetAddress,
      country,
      city,
      zipCode,
      creditCardCvv,
      creditCardExpirationMonth,
      creditCardExpirationYear,
      creditCardNumber,
    }: IFormData) => {
      setOrderError(undefined);
      try {
        const order = await placeOrder({
          userId,
          email,
          address: {
            streetAddress,
            state,
            country,
            city,
            zipCode,
          },
          userCurrency: selectedCurrency,
          creditCard: {
            creditCardCvv,
            creditCardExpirationMonth,
            creditCardExpirationYear,
            creditCardNumber,
          },
        });

        push({
          pathname: `/cart/checkout/${order.orderId}`,
          query: { order: JSON.stringify(order) },
        });
      } catch (error) {
        setOrderError('Failed to place order. Please try again.');
      }
    },
    [placeOrder, push, selectedCurrency]
  );

  return (
    <S.Container>
      <div>
        <S.Header>
          <S.CarTitle>Shopping Cart</S.CarTitle>
          <S.EmptyCartButton onClick={emptyCart} $type="link">
            Empty Cart
          </S.EmptyCartButton>
        </S.Header>
        <CartItems productList={items} />
      </div>
      {orderError && <S.ErrorMessage>{orderError}</S.ErrorMessage>}
      <CheckoutForm onSubmit={onPlaceOrder} />
    </S.Container>
  );
};

export default CartDetail;
