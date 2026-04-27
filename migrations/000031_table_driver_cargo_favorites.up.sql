--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: driver_cargo_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_cargo_favorites (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    driver_id uuid NOT NULL,
    cargo_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: driver_cargo_favorites driver_cargo_favorites_driver_id_cargo_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_driver_id_cargo_id_key UNIQUE (driver_id, cargo_id);


--
-- Name: driver_cargo_favorites driver_cargo_favorites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_pkey PRIMARY KEY (id);


--
-- Name: idx_driver_cargo_favorites_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_cargo_favorites_cargo ON public.driver_cargo_favorites USING btree (cargo_id);


--
-- Name: idx_driver_cargo_favorites_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_cargo_favorites_driver ON public.driver_cargo_favorites USING btree (driver_id);


--
-- Name: driver_cargo_favorites driver_cargo_favorites_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: driver_cargo_favorites driver_cargo_favorites_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_cargo_favorites
    ADD CONSTRAINT driver_cargo_favorites_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


